import { describe, expect, it } from "vitest";
import type { Repository } from "@/api";
import { dependencyTargetRepositories } from "@/lib/dependencyTargets";

const baseRepo = {
  description: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const monorepo: Repository = {
  ...baseRepo,
  id: "mono-1",
  name: "monorepo",
  root_path: "/mono",
  kind: "monorepo",
  sub_projects: [
    { path: "server", kind: "backend" },
    { path: "web", kind: "frontend" },
  ],
};

const plainRepo: Repository = {
  ...baseRepo,
  id: "plain-1",
  name: "plain",
  root_path: "/plain",
  kind: "backend",
};

const otherRepo: Repository = {
  ...baseRepo,
  id: "other-1",
  name: "other",
  root_path: "/other",
  kind: "frontend",
};

describe("dependencyTargetRepositories", () => {
  it("includes the current monorepo itself when picking a sub_repo target", () => {
    const repositories = [monorepo, otherRepo];
    const options = dependencyTargetRepositories(repositories, monorepo.id, "sub_repo");
    expect(options.map((r) => r.id)).toContain(monorepo.id);
  });

  it("excludes the current repository when picking a sub_repo target and it is not a monorepo", () => {
    const repositories = [plainRepo, otherRepo];
    const options = dependencyTargetRepositories(repositories, plainRepo.id, "sub_repo");
    expect(options.map((r) => r.id)).not.toContain(plainRepo.id);
  });

  it("excludes the current repository when picking a plain repo target, even for a monorepo", () => {
    const repositories = [monorepo, otherRepo];
    const options = dependencyTargetRepositories(repositories, monorepo.id, "repo");
    expect(options.map((r) => r.id)).not.toContain(monorepo.id);
    expect(options.map((r) => r.id)).toEqual([otherRepo.id]);
  });
});
