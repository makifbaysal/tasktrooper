import { describe, expect, it } from "vitest";
import { clusterPositions, defaultClusterOptions } from "@/lib/embeddingMapClusters";

function mulberry32(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function gaussian(random: () => number): number {
  const u = Math.max(random(), 1e-12);
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * random());
}

function blobs(): { positions: Float32Array; ranges: [number, number][] } {
  const random = mulberry32(42);
  const specs: [number, number, number][] = [
    [0.2, 0.2, 200],
    [0.8, 0.3, 150],
    [0.5, 0.85, 100],
  ];
  const values: number[] = [];
  const ranges: [number, number][] = [];
  for (const [cx, cy, count] of specs) {
    const start = values.length / 2;
    for (let i = 0; i < count; i++) {
      values.push(cx + gaussian(random) * 0.02, cy + gaussian(random) * 0.02);
    }
    ranges.push([start, start + count]);
  }
  for (let i = 0; i < 30; i++) values.push(random(), random());
  return { positions: Float32Array.from(values), ranges };
}

describe("clusterPositions", () => {
  it("finds three separated blobs ordered by size", () => {
    const { positions, ranges } = blobs();
    const labels = clusterPositions(positions);
    expect(new Set([...labels].filter((l) => l >= 0)).size).toBe(3);
    ranges.forEach(([start, end], expected) => {
      const counts = new Map<number, number>();
      for (let i = start; i < end; i++) counts.set(labels[i], (counts.get(labels[i]) ?? 0) + 1);
      const [label, count] = [...counts.entries()].sort((a, b) => b[1] - a[1])[0];
      expect(label).toBe(expected);
      expect(count / (end - start)).toBeGreaterThanOrEqual(0.95);
    });
  });

  it("is deterministic", () => {
    const { positions } = blobs();
    expect(Array.from(clusterPositions(positions))).toEqual(Array.from(clusterPositions(positions)));
  });

  it("handles empty and tiny inputs", () => {
    expect(clusterPositions(new Float32Array(0)).length).toBe(0);
    const small = clusterPositions(Float32Array.from([0, 0, 1, 1, 0.5, 0.5, 0.2, 0.9, 0.9, 0.1]));
    expect(Array.from(small)).toEqual([0, 0, 0, 0, 0]);
  });

  it("survives identical points", () => {
    const positions = new Float32Array(120).fill(0.5);
    const labels = clusterPositions(positions);
    expect(labels.length).toBe(60);
    expect(labels.every((l) => l === 0)).toBe(true);
  });

  it("clusters 5000 uniform points within the time budget", () => {
    const random = mulberry32(7);
    const positions = new Float32Array(10000);
    for (let i = 0; i < positions.length; i++) positions[i] = random();
    const started = performance.now();
    const labels = clusterPositions(positions);
    const elapsed = performance.now() - started;
    console.info(`clusterPositions(5000) took ${Math.round(elapsed)} ms`);
    expect(labels.length).toBe(5000);
    expect(elapsed).toBeLessThan(5000);
  });

  it("derives default options from the point count", () => {
    expect(defaultClusterOptions(2000)).toEqual({ minClusterSize: 40, minSamples: 15 });
    expect(defaultClusterOptions(100)).toEqual({ minClusterSize: 8, minSamples: 4 });
  });
});
