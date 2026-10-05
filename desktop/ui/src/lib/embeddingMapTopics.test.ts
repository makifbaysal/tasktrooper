import { describe, expect, it } from "vitest";
import {
  classifyChunkKind,
  describeTopics,
  directoryOf,
  kindComposition,
  pickDirectoryDepth,
  shortenPath,
  tokenize,
  type ChunkKind,
} from "@/lib/embeddingMapTopics";

describe("classifyChunkKind", () => {
  const cases: [string, ChunkKind][] = [
    ["backend/internal/ports/mocks/mock_store.go", "mock"],
    ["src/__mocks__/api.ts", "mock"],
    ["internal/foo/store_mock.go", "mock"],
    ["src/MockStore.java", "mock"],
    ["api/v1/service.pb.go", "generated"],
    ["internal/db/models_gen.go", "generated"],
    ["web/package-lock.json", "generated"],
    ["go.sum", "generated"],
    ["internal/foo/bar_test.go", "test"],
    ["src/components/Button.test.tsx", "test"],
    ["tests/test_api.py", "test"],
    ["src/test/java/com/acme/FooTest.java", "test"],
    ["docs/guide.md", "docs"],
    ["README.md", "docs"],
    ["config/app.yaml", "config"],
    ["Dockerfile", "config"],
    ["Makefile", "config"],
    [".env.example", "config"],
    ["internal/http/handler.go", "source"],
    ["src/latest/thing.ts", "source"],
    ["src/contest.go", "source"],
    ["src/mockingbird/app.ts", "source"],
  ];
  it.each(cases)("%s -> %s", (path, kind) => {
    expect(classifyChunkKind(path)).toBe(kind);
  });
});

describe("tokenize", () => {
  it("splits paths and camelCase", () => {
    expect(tokenize("internal/http/handlerEmbeddingMap.go")).toEqual([
      "http",
      "handler",
      "embedding",
      "map",
    ]);
    expect(tokenize("HTTPServer")).toEqual(["http", "server"]);
  });
  it("drops digits, short tokens and stopwords", () => {
    expect(tokenize("a1 12345 ab the func açık_ğüşıöç")).toEqual(["açık", "ğüşıöç"]);
  });
});

describe("directories", () => {
  it("directoryOf", () => {
    expect(directoryOf("a/b/c/file.go", 2)).toBe("a/b");
    expect(directoryOf("a/b/file.go", 5)).toBe("a/b");
    expect(directoryOf("file.go", 1)).toBe("");
  });
  it("pickDirectoryDepth", () => {
    const paths: string[] = [];
    for (let a = 0; a < 2; a++) {
      for (let b = 0; b < 5; b++) {
        paths.push(`mono/p${a}/s${b}/leaf/f.go`);
      }
    }
    expect(pickDirectoryDepth(paths.map((p) => p))).toBe(4);
    const deep: string[] = [];
    for (let i = 0; i < 10; i++) {
      for (let j = 0; j < 3; j++) deep.push(`mono/pkg/m${i}/leaf${j}/f.go`);
    }
    expect(pickDirectoryDepth(deep)).toBe(3);
    expect(pickDirectoryDepth(Array.from({ length: 20 }, (_, i) => `d${i}/f.go`))).toBe(1);
  });
});

describe("shortenPath", () => {
  it("keeps short paths", () => {
    expect(shortenPath("a/b.go")).toBe("a/b.go");
  });
  it("keeps the last two segments", () => {
    expect(shortenPath("backend/internal/adapter/ports/mock_store_implementation.go", 40)).toBe(
      "…/ports/mock_store_implementation.go",
    );
  });
  it("falls back to the basename", () => {
    expect(shortenPath("backend/internal/adapter/ports_with_a_long_name/mock_store_impl.go", 30)).toBe(
      "…/mock_store_impl.go",
    );
  });
  it("truncates a huge basename", () => {
    const out = shortenPath(`x/y/z/${"a".repeat(60)}.go`, 20);
    expect(out).toBe(`${"a".repeat(19)}…`);
  });
});

describe("kindComposition", () => {
  it("counts every kind in order", () => {
    const result = kindComposition(["source", "source", "test", "docs"]);
    expect(result.map((r) => r.kind)).toEqual(["source", "test", "mock", "generated", "docs", "config"]);
    expect(result[0]).toEqual({ kind: "source", count: 2, share: 0.5 });
    expect(result[2].share).toBe(0);
    expect(kindComposition([]).every((r) => r.share === 0)).toBe(true);
  });
});

describe("describeTopics", () => {
  it("labels clusters with distinctive terms", () => {
    const points = [
      ...["upload", "delete", "list", "get", "patch"].map((name, i) => ({
        path: `backend/internal/adapter/http/handler_${name}.go`,
        symbol: ["HandleUpload", "HandleDelete", "HandleList", "HandleGet", "HandlePatch"][i],
      })),
      ...["tasks", "users", "runs", "files", "notes"].map((name, i) => ({
        path: `backend/internal/adapter/postgres/repo_${name}.go`,
        symbol: ["QueryTasks", "QueryUsers", "InsertTask", "InsertRun", "QueryFiles"][i],
      })),
      { path: "scripts/misc.sh" },
      { path: "tools/other.py" },
    ];
    const positions = new Float32Array(points.length * 2);
    points.forEach((_, i) => {
      positions[i * 2] = i < 5 ? 0.2 + i * 0.01 : 0.8 + i * 0.01;
      positions[i * 2 + 1] = 0.5;
    });
    const clusters = Int32Array.from(points.map((_, i) => (i < 5 ? 0 : i < 10 ? 1 : -1)));

    const topics = describeTopics({ clusters, positions, points, mode: "code" });
    expect(topics.length).toBe(2);
    expect(topics.map((t) => t.size)).toEqual([5, 5]);
    expect(["backend", "adapter"]).not.toContain(topics[0].terms[0]);
    expect(topics[0].terms[0]).toMatch(/^(http|handler|handle)$/);
    expect(topics[0].topDirectory).toBe("backend/internal/adapter/http");
    expect(topics[0].topDirectoryShare).toBe(1);
    expect(topics[1].topDirectory).toBe("backend/internal/adapter/postgres");
    expect(topics[0].anchor).toBeGreaterThanOrEqual(0);
    expect(topics[0].anchor).toBeLessThan(5);
    expect(topics[1].anchor).toBeGreaterThanOrEqual(5);
    expect(topics[1].anchor).toBeLessThan(10);
  });
});
