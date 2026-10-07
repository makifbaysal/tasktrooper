import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

const { listInitiativeProjects, listRepositories, githubStatus, githubOwners, githubOwnerRepos } = vi.hoisted(() => ({
  listInitiativeProjects: vi.fn(),
  listRepositories: vi.fn(),
  githubStatus: vi.fn(),
  githubOwners: vi.fn(),
  githubOwnerRepos: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listInitiativeProjects,
      listRepositories,
      githubStatus,
      githubOwners,
      githubOwnerRepos,
    },
  };
});

import { repoNameError, sanitizeRepoName } from "@/components/projects/add/NewRepositoryForm";
import { SourceStep } from "@/components/projects/add/SourceStep";
import { I18nProvider } from "@/hooks/useI18n";

const FOLDER_PLACEHOLDER = "/path/to/project";

type OnScan = ComponentProps<typeof SourceStep>["onScan"];
type OnCreate = ComponentProps<typeof SourceStep>["onCreate"];

function renderStep(props: { onCreate?: Mock<OnCreate>; initialProjectId?: string } = {}) {
  const onScan = vi.fn<OnScan>().mockResolvedValue(undefined);
  const onCreate = props.onCreate ?? vi.fn<OnCreate>().mockResolvedValue(undefined);
  render(
    <I18nProvider>
      <MemoryRouter>
        <SourceStep initialProjectId={props.initialProjectId} onScan={onScan} onCreate={onCreate} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { onScan, onCreate };
}

function pickMode(name: RegExp) {
  fireEvent.click(screen.getByRole("radio", { name }));
}

describe("SourceStep mode picker", () => {
  beforeEach(() => {
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [{ id: "p1", name: "Acme Shop" }] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    githubStatus.mockReset().mockResolvedValue({ connected: false });
    githubOwners.mockReset().mockResolvedValue({ owners: [] });
    githubOwnerRepos.mockReset().mockResolvedValue({ repos: [] });
  });

  it("shows no source details until a mode is picked, then only that mode's", async () => {
    renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    expect(screen.getAllByRole("radio")).toHaveLength(3);
    expect(screen.queryByPlaceholderText(FOLDER_PLACEHOLDER)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Repository name")).not.toBeInTheDocument();
    expect(screen.queryByText("GitHub isn't connected")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Import|Create/ })).not.toBeInTheDocument();

    pickMode(/Choose a folder/);
    expect(screen.getByRole("radio", { name: /Choose a folder/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByPlaceholderText(FOLDER_PLACEHOLDER)).toBeInTheDocument();
    expect(screen.queryByLabelText("Repository name")).not.toBeInTheDocument();
    expect(screen.queryByText("GitHub isn't connected")).not.toBeInTheDocument();

    pickMode(/Choose from GitHub/);
    expect(await screen.findByText("GitHub isn't connected")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(FOLDER_PLACEHOLDER)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Repository name")).not.toBeInTheDocument();

    pickMode(/Create a new repository/);
    expect(screen.getByLabelText("Repository name")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(FOLDER_PLACEHOLDER)).not.toBeInTheDocument();
    expect(screen.queryByText("GitHub isn't connected")).not.toBeInTheDocument();
  });

  it("labels the primary button by mode", async () => {
    renderStep({ initialProjectId: "p1" });
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    pickMode(/Choose a folder/);
    expect(screen.getByRole("button", { name: "Import" })).toBeInTheDocument();
    pickMode(/Create a new repository/);
    expect(screen.getByRole("button", { name: "Create" })).toBeInTheDocument();
  });

  it("keeps Import disabled with no project chosen, even once a folder is filled in", async () => {
    renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    pickMode(/Choose a folder/);
    fireEvent.change(screen.getByPlaceholderText(FOLDER_PLACEHOLDER), { target: { value: "/Users/me/code/acme" } });
    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();
  });

  it("keeps Import disabled for a new project until it has a name, then imports the folder", async () => {
    const { onScan } = renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("tab", { name: "New project" }));
    pickMode(/Choose a folder/);
    fireEvent.change(screen.getByPlaceholderText(FOLDER_PLACEHOLDER), { target: { value: "/Users/me/code/acme" } });

    const importButton = screen.getByRole("button", { name: "Import" });
    expect(importButton).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("e.g. Acme Shop"), { target: { value: "Acme Shop" } });
    expect(importButton).toBeEnabled();

    fireEvent.click(importButton);
    await waitFor(() =>
      expect(onScan).toHaveBeenCalledWith({ mode: "new", name: "Acme Shop" }, { github: null, folderPath: "/Users/me/code/acme" }),
    );
  });
});

describe("SourceStep new repository", () => {
  beforeEach(() => {
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [{ id: "p1", name: "Acme Shop" }] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    githubStatus.mockReset().mockResolvedValue({ connected: true });
    githubOwners.mockReset().mockResolvedValue({ owners: [{ login: "acme", type: "org" }] });
    githubOwnerRepos.mockReset().mockResolvedValue({ repos: [] });
  });

  it("applies the server's naming rule and previews the name it will get", async () => {
    const { onCreate } = renderStep({ initialProjectId: "p1" });
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());
    pickMode(/Create a new repository/);

    const create = screen.getByRole("button", { name: "Create" });
    expect(create).toBeDisabled();
    expect(screen.getByText(/Lower-case letters, digits/)).toBeInTheDocument();

    const name = screen.getByLabelText("Repository name");
    fireEvent.change(name, { target: { value: "çğü" } });
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(/Needs at least one letter or digit/)).toBeInTheDocument();
    expect(create).toBeDisabled();

    fireEvent.change(name, { target: { value: "My Web App" } });
    expect(name).not.toHaveAttribute("aria-invalid");
    expect(screen.getByText("my-web-app")).toBeInTheDocument();
    expect(create).toBeEnabled();

    fireEvent.click(create);
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1));
    expect(onCreate.mock.calls[0][1].name).toBe("my-web-app");
  });

  it("submits exactly what the form says", async () => {
    const { onCreate, onScan } = renderStep({ initialProjectId: "p1" });
    await waitFor(() => expect(githubOwners).toHaveBeenCalled());
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());
    pickMode(/Create a new repository/);

    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: " acme-web " } });
    fireEvent.change(screen.getByLabelText("What will this repository be?"), { target: { value: "The web shop" } });
    fireEvent.change(screen.getByLabelText("Stack"), { target: { value: "Next.js (TypeScript)" } });
    fireEvent.click(screen.getByRole("switch", { name: "Scaffold the project" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Architecture" }));

    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1));
    expect(onCreate).toHaveBeenCalledWith(
      { mode: "existing", projectId: "p1", projectName: "Acme Shop" },
      {
        name: "acme-web",
        owner: "acme",
        description: "The web shop",
        role: "frontend",
        stack: "Next.js (TypeScript)",
        notes: undefined,
        scaffold: false,
        docs: ["coding_standards", "test_standards", "local_run"],
      },
    );
    expect(onScan).not.toHaveBeenCalled();
  });

  it("shows a failed create inline and retries it", async () => {
    const onCreate = vi
      .fn<OnCreate>()
      .mockRejectedValueOnce(new Error("directory already exists"))
      .mockResolvedValueOnce(undefined);
    renderStep({ initialProjectId: "p1", onCreate });
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());
    pickMode(/Create a new repository/);
    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: "acme-web" } });

    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("directory already exists")).toBeInTheDocument();
    expect(screen.getByText("Could not create the repository")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText("directory already exists")).not.toBeInTheDocument());
  });

  it("offers a way out instead of Retry when the repository was created but its setup failed", async () => {
    const message =
      'repository "acme-web" was created, but the bootstrap task failed: board unavailable — finish its setup from the repository page';
    const onCreate = vi.fn<OnCreate>().mockRejectedValueOnce(new Error(message));
    renderStep({ initialProjectId: "p1", onCreate });
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());
    pickMode(/Create a new repository/);
    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: "acme-web" } });

    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByText("The repository was created, but its setup didn't finish")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open project" })).toHaveAttribute("href", "/projects/p1");
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Repository name"), { target: { value: "acme-web-2" } });
    expect(screen.getByRole("button", { name: "Create" })).toBeEnabled();
  });
});

describe("repository name rule (mirrors domain.NewRepoDirName)", () => {
  it("sanitizes the way the server does", () => {
    expect(sanitizeRepoName("My App")).toBe("my-app");
    expect(sanitizeRepoName("  api_v2.service  ")).toBe("api_v2.service");
    expect(sanitizeRepoName("../../etc/passwd")).toBe("etcpasswd");
    expect(sanitizeRepoName("-.leading and trailing._-")).toBe("leading-and-trailing");
    expect(sanitizeRepoName("CON")).toBe("con-repo");
    expect(sanitizeRepoName("Aux.txt")).toBe("aux-repo.txt");
    expect(sanitizeRepoName("prn.tar.gz")).toBe("prn-repo.tar.gz");
    expect(sanitizeRepoName("console")).toBe("console");
    expect(sanitizeRepoName("com10")).toBe("com10");
  });

  it("refuses what the server refuses", () => {
    expect(repoNameError("")).toBe("required");
    expect(repoNameError("   ")).toBe("required");
    expect(repoNameError("..")).toBe("no_letters");
    expect(repoNameError("çğü")).toBe("no_letters");
    expect(repoNameError("a".repeat(101))).toBe("too_long");
    expect(repoNameError("My App")).toBeNull();
  });
});
