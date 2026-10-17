import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ZERO_UUID, type OpsErrorGroup, type OpsErrors, type ProjectsOverview } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { component, env, opsOverview, repo } from "@/test/opsConsoleFixtures";

const { getProjectsOverview, listIncidents, listAllReleases, listOpsErrors, createErrorTask, createBoundErrorTask, getComponentSignals, toastSuccess } =
  vi.hoisted(() => ({
    getProjectsOverview: vi.fn(),
    getComponentSignals: vi.fn(),
    listIncidents: vi.fn(),
    listAllReleases: vi.fn(),
    listOpsErrors: vi.fn(),
    createErrorTask: vi.fn(),
    createBoundErrorTask: vi.fn(),
    toastSuccess: vi.fn(),
  }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getProjectsOverview, listIncidents, listAllReleases, listOpsErrors, createErrorTask, createBoundErrorTask, getComponentSignals },
  };
});

vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: vi.fn() } }));

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import OpsErrorsInboxPage from "@/pages/ops/OpsErrorsInboxPage";
import EntityErrors from "@/pages/ops/entity/EntityErrors";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

const MINUTE = 60_000;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function group(overrides: Partial<OpsErrorGroup>): OpsErrorGroup {
  return {
    component_id: "c-api",
    repository_id: "r-shop",
    environment_id: "e-api",
    provider: "gcp",
    fingerprint: "fp",
    message: "boom",
    count: 1,
    first_seen: ago(30 * MINUTE),
    last_seen: ago(5 * MINUTE),
    new: false,
    ...overrides,
  };
}

const answer: OpsErrors = {
  groups: [
    group({
      fingerprint: "fp-new",
      message: "TypeError: cannot read properties of undefined (reading 'id')",
      count: 12,
      new: true,
      source: "handlers/orders.go:42",
      sample: "goroutine 1 [running]:\nmain.handler()",
      external_url: "https://console.cloud.google.com/errors/fp-new",
    }),
    group({
      component_id: "c-site",
      repository_id: "r-site",
      environment_id: "e-site",
      provider: "vercel",
      fingerprint: "fp-old",
      message: "Timeout calling payments",
      count: 3,
      first_seen: ago(3 * 24 * 60 * MINUTE),
    }),
  ],
  unavailable: [
    { component_id: "c-web", environment_id: "e-web", code: "cloud_auth", message: "token expired" },
    { component_id: "c-site", environment_id: "e-site", code: "provider_error", message: "upstream 503" },
  ],
  truncated: true,
};

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
            <Route path="errors" element={<OpsErrorsInboxPage />} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="errors" element={<EntitySubRoute tab="errors" page={EntityErrors} />} />
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
  getProjectsOverview.mockReset().mockResolvedValue(opsOverview);
  listIncidents.mockReset().mockResolvedValue({ incidents: [], count: 0 });
  listAllReleases.mockReset().mockResolvedValue({ releases: [] });
  listOpsErrors.mockReset().mockResolvedValue(answer);
  createErrorTask.mockReset().mockResolvedValue({ id: "t-1", key: "SHOP-7" });
  createBoundErrorTask.mockReset().mockResolvedValue({ id: "t-2", key: "SHOP-8" });
  getComponentSignals.mockReset().mockResolvedValue({ component_id: "c-worker", role: "worker", environment: "production", signals: [] });
  toastSuccess.mockReset();
});

async function groupRows() {
  const table = await screen.findByRole("table", { name: "Error groups" });
  return within(table).getAllByRole("row").slice(1);
}

describe("OpsErrorsInboxPage", () => {
  it("reads every component in the environment since the start of the range", async () => {
    renderAt("/operations/errors");
    await groupRows();
    const [query] = listOpsErrors.mock.calls[0];
    expect(query.env).toBe("production");
    expect(query.componentIds).toBeUndefined();
    const since = Date.parse(query.since);
    expect(Math.abs(Date.now() - 60 * MINUTE - since)).toBeLessThan(5_000);
  });

  it("lists groups new first with entity, count, last seen and actions", async () => {
    renderAt("/operations/errors");
    const [first, second] = await groupRows();
    expect(within(first).getByText("New")).toBeInTheDocument();
    expect(within(first).getByRole("link", { name: /TypeError: cannot read/ })).toBeInTheDocument();
    expect(within(first).getByText("handlers/orders.go:42")).toBeInTheDocument();
    expect(within(first).getByRole("link", { name: "api" })).toHaveAttribute("href", "/operations/entity/c-api/errors");
    expect(within(first).getByText("api-svc")).toBeInTheDocument();
    expect(within(first).getByText("12")).toBeInTheDocument();
    expect(within(first).getByRole("link", { name: "Open in Google Cloud" })).toHaveAttribute(
      "href",
      "https://console.cloud.google.com/errors/fp-new",
    );
    expect(within(first).getByRole("img", { name: /^Seen from / })).toBeInTheDocument();
    expect(within(second).queryByText("New")).not.toBeInTheDocument();
    expect(within(second).queryByRole("link", { name: /^Open in/ })).not.toBeInTheDocument();
    expect(screen.getByText("2 of 2 error groups")).toBeInTheDocument();
  });

  it("says which environments could not be read, why, and how to fix it", async () => {
    renderAt("/operations/errors");
    await groupRows();
    expect(screen.getByText("2 environments couldn't be read")).toBeInTheDocument();
    expect(screen.getByText("The cloud account's credentials were rejected.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Reconnect the account" })).toHaveAttribute("href", "/settings/integrations");
    expect(screen.getByText("The provider answered with an error.")).toBeInTheDocument();
    expect(screen.getByText("upstream 503")).toBeInTheDocument();
    expect(screen.getByText("Not every environment was read")).toBeInTheDocument();
  });

  it("opens a group in the side panel and creates a board task from it", async () => {
    renderAt("/operations/errors");
    const [first] = await groupRows();
    fireEvent.click(within(first).getByRole("link", { name: /TypeError: cannot read/ }));
    expect(screen.getByTestId("where")).toHaveTextContent("group=e-api.fp-new");
    const panel = await screen.findByRole("region", { name: "Error group" });
    expect(within(panel).getByText(/goroutine 1 \[running\]/)).toBeInTheDocument();
    expect(within(panel).getByText("fp-new")).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Create task" }));
    await waitFor(() => expect(createErrorTask).toHaveBeenCalledTimes(1));
    const [envId, body] = createErrorTask.mock.calls[0];
    expect(envId).toBe("e-api");
    expect(body).toMatchObject({ fingerprint: "fp-new", count: 12, new: true });
    expect(body).not.toHaveProperty("component_id");
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Task created", expect.anything()));
    fireEvent.click(within(panel).getByRole("link", { name: "Close details" }));
    await waitFor(() => expect(screen.queryByRole("region", { name: "Error group" })).not.toBeInTheDocument());
  });

  it("filters to new groups and by search, and clears them", async () => {
    renderAt("/operations/errors");
    await groupRows();
    fireEvent.click(screen.getByRole("switch"));
    expect(await groupRows()).toHaveLength(1);
    fireEvent.click(screen.getByRole("switch"));
    fireEvent.change(screen.getByRole("searchbox", { name: "Search error groups" }), { target: { value: "payments" } });
    const rows = await groupRows();
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByText("Timeout calling payments")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("searchbox", { name: "Search error groups" }), { target: { value: "zzz" } });
    fireEvent.click(await screen.findByRole("button", { name: "Clear filters" }));
    expect(await groupRows()).toHaveLength(2);
  });

  it("asks the server for the entities picked", async () => {
    renderAt("/operations/errors?entity=c-api");
    await groupRows();
    expect(listOpsErrors.mock.calls[0][0].componentIds).toEqual(["c-api"]);
    expect(screen.getByRole("button", { name: "Entities: api" })).toBeInTheDocument();
  });

  it("says when nothing errored in the range", async () => {
    listOpsErrors.mockResolvedValue({ groups: [], unavailable: [], truncated: false });
    renderAt("/operations/errors");
    expect(await screen.findByText("No errors in this range")).toBeInTheDocument();
    expect(screen.getByText(/^Nothing reported an error in Production since/)).toBeInTheDocument();
  });

  it("says there is no error feed only for a Vercel preview or a custom URL", async () => {
    const previews = repo("r-pr", "previews", [component("c-pr", "pr-web", "frontend"), component("c-url", "landing", "frontend", "landing")], [
      env("e-pr", "c-pr", { environment: "preview", provider: "vercel", resource_name: "pr-web" }),
      env("e-url", "c-url", { environment: "preview", url: "https://landing.example.com" }),
    ]);
    getProjectsOverview.mockResolvedValue({ projects: [], unassigned: [previews] } as unknown as ProjectsOverview);
    listOpsErrors.mockResolvedValue({ groups: [], unavailable: [], truncated: false });
    renderAt("/operations/errors?env=preview");
    expect(
      await screen.findByText("The environments connected in Preview are a Vercel preview or a custom URL, which have no error feed to read."),
    ).toBeInTheDocument();
  });

  it("links an entity whose role has no Errors section to the inbox filtered to it", async () => {
    const data = repo("r-etl", "etl", [component("c-etl", "etl", "data")], [env("e-etl", "c-etl", { provider: "aws", resource_name: "etl-job" })]);
    getProjectsOverview.mockResolvedValue({ ...opsOverview, unassigned: [data] } as unknown as ProjectsOverview);
    listOpsErrors.mockResolvedValue({
      groups: [group({ component_id: "c-etl", repository_id: "r-etl", environment_id: "e-etl", provider: "aws", fingerprint: "fp-etl" })],
      unavailable: [],
      truncated: false,
    });
    renderAt("/operations/errors");
    const [row] = await groupRows();
    expect(within(row).getByRole("link", { name: "etl" })).toHaveAttribute("href", "/operations/errors?entity=c-etl");
  });

  it("reads even when nothing runs in this one, and points to connecting an environment when nothing answers", async () => {
    const bare = { projects: [], unassigned: [] } as unknown as ProjectsOverview;
    getProjectsOverview.mockResolvedValue(bare);
    listOpsErrors.mockResolvedValue({ groups: [], unavailable: [], truncated: false });
    renderAt("/operations/errors?env=staging");
    expect(await screen.findByText("Nothing is connected in Staging")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to entities" })).toHaveAttribute("href", "/operations?env=staging");
    expect(listOpsErrors).toHaveBeenCalledWith(expect.objectContaining({ env: "staging" }));
  });

  it("lists the groups of a component only a binding feeds, with no environment connected anywhere", async () => {
    const bare = { projects: [], unassigned: [] } as unknown as ProjectsOverview;
    getProjectsOverview.mockResolvedValue(bare);
    listOpsErrors.mockResolvedValue({
      groups: [group({ environment_id: ZERO_UUID, data_source_id: "ds-sentry", provider: "sentry", fingerprint: "77", message: "Bound only" })],
      unavailable: [],
      truncated: false,
    });
    renderAt("/operations/errors");
    const [row] = await groupRows();
    expect(within(row).getByRole("link", { name: "Bound only" })).toBeInTheDocument();
  });
});

describe("EntityErrors", () => {
  it("reads only the entity's component and hides the entity filters", async () => {
    renderAt("/operations/entity/c-api/errors");
    await groupRows();
    expect(listOpsErrors.mock.calls[0][0].componentIds).toEqual(["c-api"]);
    expect(screen.queryByRole("button", { name: /^Entities:/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Entity" })).not.toBeInTheDocument();
  });

  it("says the entity is not reporting where it has no environment, without a read", async () => {
    renderAt("/operations/entity/c-worker/errors");
    expect(await screen.findByText("Not reporting in Production")).toBeInTheDocument();
    for (const link of screen.getAllByRole("link", { name: "Connect an environment" })) {
      expect(link).toHaveAttribute("href", "/repositories/r-tools?tab=deploy");
    }
    expect(listOpsErrors).not.toHaveBeenCalled();
  });
});

describe("Sentry groups", () => {
  const SENTRY = "ds-sentry";
  const sentryGroup = group({
    environment_id: ZERO_UUID,
    data_source_id: SENTRY,
    provider: "sentry",
    fingerprint: "4711",
    message: "NSInvalidArgumentException",
    source: "AppDelegate.swift",
    external_url: "https://sentry.io/organizations/acme/issues/4711/",
  });

  it("marks a group read through Sentry, opens it there and makes a task from it through its component", async () => {
    listOpsErrors.mockResolvedValue({ groups: [sentryGroup], unavailable: [], truncated: false });
    renderAt("/operations/errors?env=staging");
    const [row] = await groupRows();
    expect(within(row).getByText("Sentry", { selector: "div" })).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Open in Sentry" })).toHaveAttribute("href", sentryGroup.external_url);
    expect(within(row).getByRole("button", { name: "Create task" })).toBeEnabled();

    fireEvent.click(within(row).getByRole("link", { name: "NSInvalidArgumentException" }));
    const panel = await screen.findByRole("region", { name: "Error group" });
    expect(within(panel).getByRole("link", { name: "Sentry data source" })).toHaveAttribute("href", `/operations/sources?source=${SENTRY}`);
    expect(within(panel).queryByRole("link", { name: "Environment settings" })).not.toBeInTheDocument();
    expect(screen.getByTestId("where")).toHaveTextContent(`group=${SENTRY}.4711`);
    fireEvent.click(within(panel).getByRole("button", { name: "Create task" }));
    await waitFor(() => expect(createBoundErrorTask).toHaveBeenCalledTimes(1));
    const [componentId, body] = createBoundErrorTask.mock.calls[0];
    expect(componentId).toBe("c-api");
    expect(body).toMatchObject({
      data_source_id: SENTRY,
      environment: "staging",
      fingerprint: "4711",
      message: "NSInvalidArgumentException",
      external_url: sentryGroup.external_url,
    });
    expect(createErrorTask).not.toHaveBeenCalled();
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Task created", expect.anything()));
  });

  it("links a binding the inbox could not read to its data source", async () => {
    listOpsErrors.mockResolvedValue({
      groups: [],
      unavailable: [{ component_id: "c-api", environment_id: ZERO_UUID, data_source_id: SENTRY, code: "provider_error", message: "401" }],
      truncated: false,
    });
    renderAt("/operations/errors");
    expect(await screen.findByRole("link", { name: "Open the data source" })).toHaveAttribute("href", `/operations/sources?source=${SENTRY}`);
  });

  it("reads an entity with no environment when Sentry is bound to its errors", async () => {
    getComponentSignals.mockResolvedValue({
      component_id: "c-worker",
      role: "worker",
      environment: "production",
      signals: [{ signal: "errors.issues", kind: "error", unit: "issues", golden: false, status: "bound", binding: { id: "b-s" }, suggestions: [] }],
    });
    listOpsErrors.mockResolvedValue({ groups: [{ ...sentryGroup, component_id: "c-worker" }], unavailable: [], truncated: false });
    renderAt("/operations/entity/c-worker/errors");
    await groupRows();
    expect(listOpsErrors.mock.calls[0][0].componentIds).toEqual(["c-worker"]);
  });
});
