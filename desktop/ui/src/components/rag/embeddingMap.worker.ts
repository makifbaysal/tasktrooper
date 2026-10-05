import { UMAP } from "umap-js";
import { clusterPositions, defaultClusterOptions } from "@/lib/embeddingMapClusters";
import {
  clampNeighbors,
  normalizePositions,
  type EmbeddingMapWorkerRequest,
  type EmbeddingMapWorkerResponse,
} from "@/lib/embeddingMap";

// The app's tsconfig only ships the DOM lib, so `self` is typed as a Window.
// Narrow it to the two worker APIs we actually use instead of reaching for
// `any` — this keeps both message directions fully typed.
interface WorkerScope {
  postMessage(message: EmbeddingMapWorkerResponse, transfer?: Transferable[]): void;
  addEventListener(
    type: "message",
    listener: (event: MessageEvent<EmbeddingMapWorkerRequest>) => void,
  ): void;
}

const ctx = self as unknown as WorkerScope;

/** Deterministic RNG so the same input produces the same layout every run. */
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

function toRows(vectors: Float32Array, count: number, dims: number): number[][] {
  const rows: number[][] = new Array(count);
  for (let i = 0; i < count; i++) {
    const row: number[] = new Array(dims);
    const base = i * dims;
    for (let d = 0; d < dims; d++) {
      const value = vectors[base + d];
      row[d] = Number.isFinite(value) ? value : 0;
    }
    rows[i] = row;
  }
  return rows;
}

/** 1-2 points cannot be projected; place them by hand so the plot still renders. */
function trivialLayout(count: number): Float32Array {
  const positions = new Float32Array(count * 2);
  if (count === 1) {
    positions[0] = 0.5;
    positions[1] = 0.5;
  } else if (count === 2) {
    positions[0] = 0.3;
    positions[1] = 0.5;
    positions[2] = 0.7;
    positions[3] = 0.5;
  }
  return positions;
}

function project(request: EmbeddingMapWorkerRequest): void {
  const { requestId, count, dims } = request;

  if (count === 0) {
    ctx.postMessage({
      type: "done",
      requestId,
      positions: new Float32Array(0),
      clusters: new Int32Array(0),
    });
    return;
  }
  if (count <= 2) {
    const positions = trivialLayout(count);
    const clusters = new Int32Array(count);
    ctx.postMessage({ type: "done", requestId, positions, clusters }, [
      positions.buffer,
      clusters.buffer,
    ]);
    return;
  }

  const rows = toRows(request.vectors, count, dims);

  ctx.postMessage({ type: "progress", requestId, phase: "neighbors", ratio: 0 });

  const umap = new UMAP({
    nComponents: 2,
    nNeighbors: clampNeighbors(request.nNeighbors, count),
    minDist: request.minDist,
    random: mulberry32(0x5eed),
  });

  // Neighbour search happens here and is the slow, non-incremental phase.
  const totalEpochs = umap.initializeFit(rows);
  ctx.postMessage({ type: "progress", requestId, phase: "layout", ratio: 0 });

  const reportEvery = Math.max(1, Math.floor(totalEpochs / 50));
  for (let epoch = 0; epoch < totalEpochs; epoch++) {
    umap.step();
    if (epoch % reportEvery === 0) {
      ctx.postMessage({
        type: "progress",
        requestId,
        phase: "layout",
        ratio: epoch / totalEpochs,
      });
    }
  }

  const positions = normalizePositions(umap.getEmbedding());
  ctx.postMessage({ type: "progress", requestId, phase: "clusters", ratio: 0 });
  const clusters = clusterPositions(positions, defaultClusterOptions(count));
  ctx.postMessage({ type: "done", requestId, positions, clusters }, [
    positions.buffer,
    clusters.buffer,
  ]);
}

ctx.addEventListener("message", (event) => {
  const request = event.data;
  try {
    project(request);
  } catch (error) {
    ctx.postMessage({
      type: "error",
      requestId: request.requestId,
      message: error instanceof Error ? error.message : String(error),
    });
  }
});
