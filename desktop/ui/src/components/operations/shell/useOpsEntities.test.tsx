import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

const { getProjectsOverview } = vi.hoisted(() => ({ getProjectsOverview: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getProjectsOverview } };
});

import {
  CACHE_OPS_OVERVIEW,
  buildOpsDirectory,
  toOpsEntities,
  useOpsEntities,
  useOpsEntity,
} from "@/components/operations/shell/useOpsEntities";

describe("buildOpsDirectory / toOpsEntities", () => {
  const directory = buildOpsDirectory(opsOverview);

  it("maps each component to an entity with its repository, projects and confirmed environments only", () => {
    const api = directory.listed.find((e) => e.id === "c-api")!;
    expect(api).toMatchObject({ name: "api", role: "backend", repositoryId: "r-shop", repositoryName: "shop", projectIds: ["p-shop"] });
    expect(api.environments.map((e) => e.environment)).toEqual(["production", "staging"]);
    const web = directory.listed.find((e) => e.id === "c-web")!;
    expect(web.environments.map((e) => e.id)).toEqual(["e-web"]);
  });

  it("lists running roles even unconnected, and leaves libraries and CLIs out of the list", () => {
    expect(toOpsEntities(opsOverview).map((e) => e.name)).toEqual(["api", "jobs", "site", "web"]);
    expect(directory.all.map((e) => e.id)).toEqual(expect.arrayContaining(["c-lib", "c-cli", "c-docs"]));
  });

  it("knows every repository, with or without a listed component", () => {
    expect(directory.repositories.map((r) => [r.id, r.componentCount])).toEqual([
      ["r-docs", 1],
      ["r-shop", 3],
      ["r-site", 1],
      ["r-tools", 2],
    ]);
    expect(directory.projects.map((p) => p.id)).toEqual(["p-shop", "p-tools"]);
  });

  it("is empty before the overview arrives", () => {
    expect(toOpsEntities(null)).toEqual([]);
  });
});

function Entities() {
  const { entities, loading } = useOpsEntities();
  return <p data-testid="entities">{loading ? "loading" : entities.map((e) => e.id).join(",")}</p>;
}

function Entity({ id }: { id?: string }) {
  const { entity, envFor, loading } = useOpsEntity(id);
  if (loading) return <p>loading</p>;
  return (
    <p data-testid="entity">
      {entity ? `${entity.name}|${envFor("staging")?.id ?? "-"}|${envFor("preview")?.id ?? "-"}` : "none"}
    </p>
  );
}

describe("useOpsEntities / useOpsEntity", () => {
  beforeEach(() => {
    sessionStorage.clear();
    writeCache(CACHE_OPS_OVERVIEW, undefined);
    getProjectsOverview.mockReset();
    getProjectsOverview.mockResolvedValue(opsOverview);
  });

  it("loads the overview once for every subscriber", async () => {
    render(
      <MemoryRouter>
        <Entities />
        <Entity id="c-api" />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByTestId("entities")).toHaveTextContent("c-api,c-worker,c-site,c-web"));
    expect(screen.getByTestId("entity")).toHaveTextContent("api|e-api-stg|-");
    expect(getProjectsOverview).toHaveBeenCalledTimes(1);
  });

  it("finds a component that is not listed, and answers none for an unknown id", async () => {
    const { rerender } = render(
      <MemoryRouter>
        <Entities />
        <Entity id="c-lib" />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByTestId("entity")).toHaveTextContent("ui-kit|-|-"));
    rerender(
      <MemoryRouter>
        <Entities />
        <Entity id="missing" />
      </MemoryRouter>,
    );
    expect(screen.getByTestId("entity")).toHaveTextContent("none");
  });
});
