import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Incident, ProjectsOverview } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { component, env, incident, opsIncidents, opsOverview, opsReleases, repo } from "@/test/opsConsoleFixtures";

vi.mock("uplot", () => ({
  default: class {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

const { getProjectsOverview, listIncidents, listAllReleases } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import EntityChanges from "@/pages/ops/entity/EntityChanges";
import EntityErrors from "@/pages/ops/entity/EntityErrors";
import EntityIssues from "@/pages/ops/entity/EntityIssues";
import EntityLogs from "@/pages/ops/entity/EntityLogs";
import EntityQueues from "@/pages/ops/entity/EntityQueues";
import EntityReleases from "@/pages/ops/entity/EntityReleases";
import EntitySummary from "@/pages/ops/entity/EntitySummary";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";
import { EntityIndexRedirect } from "@/pages/ops/opsRedirects";

const appsRepo = repo(
  "r-apps",
  "apps",
  [component("c-ios", "ios", "mobile", "ios"), component("c-queue", "mailer", "worker", "mailer")],
  [env("e-ios", "c-ios", { provider: "aws", resource_name: "ios-store" })],
);

const overview = {
  ...opsOverview,
  unassigned: [...(opsOverview as unknown as { unassigned: unknown[] }).unassigned, appsRepo],
} as unknown as ProjectsOverview;

const incidents: Incident[] = [...opsIncidents, incident("i-preview", "r-shop", "medium", { env: "preview", title: "Preview is down" })];

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route index element={<p>all entities</p>} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route index element={<EntityIndexRedirect />} />
              <Route path="summary" element={<EntitySubRoute tab="summary" page={EntitySummary} />} />
              <Route path="queues" element={<EntitySubRoute tab="queues" page={EntityQueues} />} />
              <Route path="releases" element={<EntitySubRoute tab="releases" page={EntityReleases} />} />
              <Route path="errors" element={<EntitySubRoute tab="errors" page={EntityErrors} />} />
              <Route path="logs" element={<EntitySubRoute tab="logs" page={EntityLogs} />} />
              <Route path="changes" element={<EntitySubRoute tab="changes" page={EntityChanges} />} />
              <Route path="issues" element={<EntitySubRoute tab="issues" page={EntityIssues} />} />
              <Route path="*" element={<EntityIndexRedirect />} />
            </Route>
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of ["operations.entities.overview", "operations.console.openIncidents", "operations.console.shippedReleases"]) {
    writeCache(key, undefined);
  }
  getProjectsOverview.mockReset().mockResolvedValue(overview);
  listIncidents.mockReset().mockImplementation((params?: { repositoryId?: string }) => {
    const list = params?.repositoryId
      ? incidents.filter((i) => i.repository_id === params.repositoryId)
      : incidents.filter((i) => !["resolved", "ignored"].includes(i.status));
    return Promise.resolve({ incidents: list, count: list.length });
  });
  listAllReleases.mockReset().mockResolvedValue({ releases: opsReleases });
});

async function sections(name: string) {
  const nav = (await screen.findAllByRole("navigation", { name: `${name} sections` }))[0];
  return within(nav)
    .getAllByRole("link")
    .map((a) => a.textContent);
}

const DAY = Date.parse("2026-10-15T00:00:00Z");
const ON_THE_15TH = `range=custom&from=${DAY}&to=${DAY + 86_400_000}`;

describe("OpsEntityPage", () => {
  it("opens on Summary, keeping the console's params", async () => {
    renderAt("/operations/entity/c-api?env=staging");
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("/operations/entity/c-api/summary?env=staging"));
    expect(await screen.findByRole("heading", { level: 1, name: "api" })).toBeInTheDocument();
  });

  it("gives each role its own sections", async () => {
    renderAt("/operations/entity/c-api/summary");
    expect(await sections("api")).toEqual(["Summary", "Errors", "Logs", "Changes", "Issues"]);
  });

  it("adds Queues for a worker and Releases for a mobile app", async () => {
    const worker = renderAt("/operations/entity/c-queue/summary");
    expect(await sections("mailer")).toEqual(["Summary", "Queues", "Errors", "Logs", "Changes", "Issues"]);
    worker.unmount();
    const mobile = renderAt("/operations/entity/c-ios/summary");
    expect(await sections("ios")).toEqual(["Summary", "Releases", "Errors", "Logs", "Changes", "Issues"]);
    mobile.unmount();
    renderAt("/operations/entity/c-lib/summary");
    expect(await sections("ui-kit")).toEqual(["Summary", "Changes", "Issues"]);
  });

  it("sends a section the role does not have back to Summary", async () => {
    renderAt("/operations/entity/c-api/queues?range=6h");
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("/operations/entity/c-api/summary?range=6h"));
  });

  it("renders the worker's Queues as the way to connect a data source", async () => {
    renderAt("/operations/entity/c-queue/queues");
    expect(await screen.findByText("Connect a data source to see queues")).toBeInTheDocument();
  });

  it("keeps the environment picked even where the entity is not connected, with that environment's issues", async () => {
    renderAt("/operations/entity/c-api/summary?env=preview");
    expect(await screen.findByText("Not reporting in Preview")).toBeInTheDocument();
    expect(screen.getAllByText("Not connected in Preview").length).toBeGreaterThan(0);
    expect(screen.getByTestId("where")).toHaveTextContent("env=preview");
    const tile = screen.getByText("Open issues", { selector: "p" }).closest("div") as HTMLElement;
    await waitFor(() => expect(within(tile).getByText("1")).toBeInTheDocument());
    expect(screen.getByRole("heading", { level: 1, name: "api" }).parentElement).toHaveTextContent("Warning");
  });

  it("shows golden metrics blank until bound, the window's deploys and its activity", async () => {
    renderAt(`/operations/entity/c-api/summary?${ON_THE_15TH}`);
    expect(await screen.findByRole("heading", { level: 3, name: /^Response time/ })).toBeInTheDocument();
    expect(screen.getAllByTitle("No data source bound").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /^Time range/ })).toBeInTheDocument();
    expect(screen.getByText("Compare with previous period")).toBeInTheDocument();

    const lane = await screen.findByRole("list", { name: "Deploys in this range" });
    const marker = await within(lane).findByRole("link", { name: /^Deploy 1\.4\.0/ });
    expect(marker).toHaveAttribute("href", expect.stringContaining("/operations/entity/c-api/changes?release=1.4.0"));
    expect(within(lane).queryByRole("link", { name: /pre-1|1\.5\.0-rc/ })).not.toBeInTheDocument();

    const activity = screen.getByRole("list", { name: "Activity" });
    await waitFor(() => expect(within(activity).getByText("Incident i-shop")).toBeInTheDocument());
    expect(within(activity).getByText("1.4.0")).toBeInTheDocument();
    expect(within(activity).queryByText("Incident i-pre")).not.toBeInTheDocument();
    expect(listAllReleases).toHaveBeenCalledWith(expect.objectContaining({ componentId: "c-api", limit: 100 }));
  });

  it("reads a sole component's releases by repository", async () => {
    renderAt(`/operations/entity/c-site/summary?${ON_THE_15TH}`);
    await screen.findByRole("heading", { level: 1, name: "site" });
    await waitFor(() => expect(listAllReleases).toHaveBeenCalledWith(expect.objectContaining({ repositoryId: "r-site" })));
  });

  it("says when the entity does not exist", async () => {
    renderAt("/operations/entity/nope/summary");
    expect(await screen.findByText("Entity not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to all entities" })).toHaveAttribute("href", "/operations");
  });

  it("keeps the shell up for an address that does not decode", async () => {
    renderAt("/operations/entity/%E0%A4/summary");
    expect(await screen.findByText("Entity not found")).toBeInTheDocument();
  });

  it("says a failed first read is a failure, not a missing entity, and tries again", async () => {
    getProjectsOverview.mockReset().mockRejectedValueOnce(new Error("server is restarting")).mockResolvedValue(overview);
    renderAt("/operations/entity/c-api/summary");
    expect(await screen.findByText("Couldn't load this entity")).toBeInTheDocument();
    expect(screen.getByText("server is restarting")).toBeInTheDocument();
    expect(screen.queryByText("Entity not found")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("heading", { level: 1, name: "api" })).toBeInTheDocument();
  });
});
