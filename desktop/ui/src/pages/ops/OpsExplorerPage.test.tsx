import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Incident, ProjectsOverview, StoreAppView } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { incident, opsIncidents, opsOverview, opsReleases } from "@/test/opsConsoleFixtures";

const { getProjectsOverview, listIncidents, listAllReleases, listAllStoreApps } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  listAllStoreApps: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases, listAllStoreApps } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsExplorerPage from "@/pages/ops/OpsExplorerPage";

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
            <Route index element={<OpsExplorerPage />} />
            <Route path="services" element={<OpsExplorerPage type="services" />} />
            <Route path="mobile" element={<OpsExplorerPage type="mobile" />} />
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

const open = (list: Incident[]) => list.filter((i) => !["resolved", "ignored"].includes(i.status));

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of ["operations.entities.overview", "operations.console.openIncidents", "operations.console.shippedReleases", "operations.mobileReleases.apps"]) {
    writeCache(key, undefined);
  }
  getProjectsOverview.mockReset().mockResolvedValue(opsOverview);
  listIncidents.mockReset().mockResolvedValue({ incidents: open(opsIncidents), count: 4 });
  listAllReleases.mockReset().mockResolvedValue({ releases: opsReleases });
  listAllStoreApps.mockReset().mockResolvedValue([]);
});

async function rows() {
  const table = await screen.findByRole("table", { name: "Entities" });
  await waitFor(() => expect(within(table).getAllByRole("row").length).toBeGreaterThan(1));
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .filter((r) => within(r).queryAllByRole("link").length > 0);
}

const rowNames = async () => (await rows()).map((r) => within(r).getAllByRole("link")[0].textContent);

describe("OpsExplorerPage — All entities", () => {
  it("lists every entity worst first, with a repository row for an incident no component carries", async () => {
    renderAt("/operations");
    await waitFor(async () => expect(await rowNames()).toEqual(["api", "docs", "site", "web", "jobs"]));
    expect(listAllReleases).toHaveBeenCalledWith({
      statuses: ["deploying", "verifying", "awaiting_verdict", "rolling_back", "released", "rolled_back", "failed"],
      limit: 100,
    });
    const api = (await rows())[0];
    expect(within(api).getByRole("link", { name: "api" })).toHaveAttribute("href", "/operations/entity/c-api/summary");
    expect(within(api).getByText(/^Critical: Open critical or high issues: 1$/)).toBeInTheDocument();
    expect(within(api).getByRole("link", { name: "1 open issues for api" })).toHaveAttribute("href", "/operations/issues?entity=c-api");
    expect(within(api).getByText("1.4.0")).toBeInTheDocument();
    const docs = (await rows())[1];
    expect(within(docs).getByText("Repository-level issues")).toBeInTheDocument();
    expect(within(docs).getByRole("link", { name: "docs" })).toHaveAttribute("href", "/operations/issues?repository=r-docs");
  });

  it("counts health facets and every open incident in the environment", async () => {
    renderAt("/operations");
    await rows();
    const strip = screen.getByRole("group", { name: "Filter by health" });
    expect(within(within(strip).getByRole("button", { name: /^Critical/ })).getByText("4")).toBeInTheDocument();
    expect(within(within(strip).getByRole("button", { name: /^Warning/ })).getByText("1")).toBeInTheDocument();
    const issues = within(strip).getByRole("link", { name: "3 open issues in Production" });
    expect(issues).toHaveAttribute("href", "/operations/issues");
  });

  it("counts open issues in an environment it does not know apart instead of dropping them", async () => {
    const canary = incident("i-canary", "r-shop", "high", { env: "eu-canary" });
    listIncidents.mockResolvedValue({ incidents: [...open(opsIncidents), canary], count: 5 });
    renderAt("/operations");
    await rows();
    expect(await screen.findByText("1 open issues are in other environments")).toBeInTheDocument();
    expect(screen.getByText(/Their environment label \(eu-canary\)/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Show them in Issues" })).toHaveAttribute("href", "/operations/issues");
    expect(within(screen.getByRole("group", { name: "Filter by health" })).getByRole("link", { name: "3 open issues in Production" })).toBeInTheDocument();
  });

  it("filters by a health facet and keeps the choice in the URL", async () => {
    renderAt("/operations");
    await rows();
    const warning = within(screen.getByRole("group", { name: "Filter by health" })).getByRole("button", { name: /^Warning/ });
    fireEvent.click(warning);
    expect(warning).toHaveAttribute("aria-pressed", "true");
    expect(await rowNames()).toEqual(["jobs"]);
    expect(screen.getByTestId("where")).toHaveTextContent("/operations?health=warning");
  });

  it("filters by type, project and search", async () => {
    renderAt("/operations");
    await rows();
    fireEvent.click(within(screen.getByRole("group", { name: "Filter by type" })).getByRole("button", { name: /^Web/ }));
    expect(await rowNames()).toEqual(["site", "web"]);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search entities" }), { target: { value: "shop" } });
    expect(await rowNames()).toEqual(["web"]);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search entities" }), { target: { value: "zzz" } });
    expect(await screen.findByText("No entities match these filters")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(await rowNames()).toEqual(["api", "docs", "site", "web", "jobs"]);
  });

  it("says the time range does not drive this page instead of offering the picker", async () => {
    renderAt("/operations");
    await rows();
    expect(screen.getByText("Errors cover the last 24 h; issues are the ones open now")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Time range/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Compare with previous period")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Environment" })).toBeInTheDocument();
  });

  it("reads the environment from the URL and offers to connect what does not run there", async () => {
    renderAt("/operations?env=staging");
    await waitFor(async () => expect(await rowNames()).toEqual(["api", "jobs", "site", "web"]));
    const web = (await rows()).find((r) => within(r).queryByRole("link", { name: "web" }))!;
    expect(within(web).getByText(/^Not reporting/)).toBeInTheDocument();
    expect(within(web).getByRole("link", { name: "Connect an environment for web" })).toHaveAttribute("href", "/repositories/r-shop?tab=deploy");
    expect(within((await rows())[0]).getByRole("link", { name: "api" })).toHaveAttribute("href", "/operations/entity/c-api/summary?env=staging");
  });

  it("has an empty state when nothing is watched", async () => {
    getProjectsOverview.mockResolvedValue({ projects: [], unassigned: [] } as unknown as ProjectsOverview);
    listIncidents.mockResolvedValue({ incidents: [], count: 0 });
    renderAt("/operations");
    expect(await screen.findByText("Nothing to monitor yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to projects" })).toHaveAttribute("href", "/projects");
  });
});

describe("OpsExplorerPage — type lists", () => {
  it("shows only its type, with the type's golden metric columns blank until a source is bound", async () => {
    renderAt("/operations/services");
    await waitFor(async () => expect(await rowNames()).toEqual(["api"]));
    const table = screen.getByRole("table", { name: "Entities" });
    expect(within(table).getByRole("columnheader", { name: /Response time/ })).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: /Throughput/ })).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: /Error rate/ })).toBeInTheDocument();
    expect(within(table).getAllByText("No data source bound")).toHaveLength(3);
    expect(screen.queryByRole("group", { name: "Filter by type" })).not.toBeInTheDocument();
  });

  it("has its own empty state", async () => {
    renderAt("/operations/mobile");
    expect(await screen.findByText("No mobile or desktop apps yet")).toBeInTheDocument();
  });

  it("lists store apps no mobile component carries, linking to the repository's store page", async () => {
    const app = (platform: "ios" | "android") =>
      ({ id: `a-${platform}`, repository_id: "r-tools", repository_name: "tools", platform, identifier: "com.acme.tools", state: "live" }) as StoreAppView;
    listAllStoreApps.mockResolvedValue([app("android"), app("ios")]);
    renderAt("/operations/mobile");
    await waitFor(async () => expect(await rowNames()).toEqual(["tools"]));
    const [row] = await rows();
    expect(within(row).getByRole("link", { name: "tools" })).toHaveAttribute("href", "/operations/mobile/app/r-tools");
    expect(within(row).getByText("iOS · Android · no mobile component")).toBeInTheDocument();
  });
});
