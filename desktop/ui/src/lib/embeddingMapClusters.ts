// HDBSCAN over the 2D embedding-map layout. Imported by the UMAP worker, so it
// must stay free of React and DOM APIs.

export const NOISE_CLUSTER = -1;

export interface ClusterOptions {
  /** Smallest group of points that counts as a cluster. */
  minClusterSize: number;
  /** k for the core distance (density estimate). */
  minSamples: number;
}

const MIN_DISTANCE = 1e-12;

function clamp(value: number, low: number, high: number): number {
  return Math.max(low, Math.min(high, value));
}

export function defaultClusterOptions(count: number): ClusterOptions {
  const minClusterSize = clamp(Math.round(count * 0.02), 8, 80);
  const minSamples = clamp(Math.round(minClusterSize / 2), 4, 15);
  return { minClusterSize, minSamples };
}

/** Squared distance to the k-th nearest other point, per point. */
function coreDistancesSq(positions: Float32Array, n: number, k: number): Float64Array {
  const core = new Float64Array(n);
  const nearest = new Float64Array(k);
  for (let i = 0; i < n; i++) {
    nearest.fill(Number.POSITIVE_INFINITY);
    const xi = positions[i * 2];
    const yi = positions[i * 2 + 1];
    for (let j = 0; j < n; j++) {
      if (j === i) continue;
      const dx = positions[j * 2] - xi;
      const dy = positions[j * 2 + 1] - yi;
      const d = dx * dx + dy * dy;
      if (d >= nearest[k - 1]) continue;
      let slot = k - 1;
      while (slot > 0 && nearest[slot - 1] > d) {
        nearest[slot] = nearest[slot - 1];
        slot--;
      }
      nearest[slot] = d;
    }
    core[i] = nearest[k - 1];
  }
  return core;
}

interface MstEdges {
  a: Int32Array;
  b: Int32Array;
  weightSq: Float64Array;
}

/** Prim over the implicit complete graph; memory stays O(n). */
function mutualReachabilityMst(positions: Float32Array, n: number, coreSq: Float64Array): MstEdges {
  const a = new Int32Array(n - 1);
  const b = new Int32Array(n - 1);
  const weightSq = new Float64Array(n - 1);
  const inTree = new Uint8Array(n);
  const best = new Float64Array(n).fill(Number.POSITIVE_INFINITY);
  const from = new Int32Array(n);

  let current = 0;
  inTree[0] = 1;
  for (let step = 0; step < n - 1; step++) {
    const xc = positions[current * 2];
    const yc = positions[current * 2 + 1];
    const coreCurrent = coreSq[current];
    let next = -1;
    let nextWeight = Number.POSITIVE_INFINITY;
    for (let j = 0; j < n; j++) {
      if (inTree[j]) continue;
      const dx = positions[j * 2] - xc;
      const dy = positions[j * 2 + 1] - yc;
      let w = dx * dx + dy * dy;
      if (coreCurrent > w) w = coreCurrent;
      if (coreSq[j] > w) w = coreSq[j];
      if (w < best[j]) {
        best[j] = w;
        from[j] = current;
      }
      if (best[j] < nextWeight) {
        nextWeight = best[j];
        next = j;
      }
    }
    a[step] = from[next];
    b[step] = next;
    weightSq[step] = nextWeight;
    inTree[next] = 1;
    current = next;
  }
  return { a, b, weightSq };
}

interface LinkageTree {
  left: Int32Array;
  right: Int32Array;
  distance: Float64Array;
  size: Int32Array;
}

/** Internal node n + i is the i-th merge; leaves are 0..n-1. */
function singleLinkage(edges: MstEdges, n: number): LinkageTree {
  const order = Array.from({ length: n - 1 }, (_, i) => i);
  order.sort((p, q) => {
    if (edges.weightSq[p] !== edges.weightSq[q]) return edges.weightSq[p] - edges.weightSq[q];
    const pLow = Math.min(edges.a[p], edges.b[p]);
    const qLow = Math.min(edges.a[q], edges.b[q]);
    if (pLow !== qLow) return pLow - qLow;
    return Math.max(edges.a[p], edges.b[p]) - Math.max(edges.a[q], edges.b[q]);
  });

  const left = new Int32Array(n - 1);
  const right = new Int32Array(n - 1);
  const distance = new Float64Array(n - 1);
  const size = new Int32Array(2 * n - 1).fill(1);
  const parent = new Int32Array(n);
  const componentNode = new Int32Array(n);
  for (let i = 0; i < n; i++) {
    parent[i] = i;
    componentNode[i] = i;
  }
  const find = (x: number): number => {
    let root = x;
    while (parent[root] !== root) root = parent[root];
    while (parent[x] !== root) {
      const up = parent[x];
      parent[x] = root;
      x = up;
    }
    return root;
  };

  for (let i = 0; i < n - 1; i++) {
    const edge = order[i];
    const ra = find(edges.a[edge]);
    const rb = find(edges.b[edge]);
    const nodeA = componentNode[ra];
    const nodeB = componentNode[rb];
    left[i] = nodeA;
    right[i] = nodeB;
    distance[i] = Math.sqrt(edges.weightSq[edge]);
    size[n + i] = size[nodeA] + size[nodeB];
    parent[rb] = ra;
    componentNode[ra] = n + i;
  }
  return { left, right, distance, size };
}

/**
 * Per-point cluster id for `positions` (`2 * n` interleaved x/y in [0, 1]):
 * 0..k-1 ordered by cluster size (biggest first), NOISE_CLUSTER for noise.
 */
export function clusterPositions(
  positions: Float32Array,
  options?: ClusterOptions,
): Int32Array {
  const n = positions.length / 2;
  if (n === 0) return new Int32Array(0);
  const { minClusterSize, minSamples } = options ?? defaultClusterOptions(n);
  if (n < 2 * minClusterSize) return new Int32Array(n);

  const k = Math.max(1, Math.min(minSamples, n - 1));
  const coreSq = coreDistancesSq(positions, n, k);
  const tree = singleLinkage(mutualReachabilityMst(positions, n, coreSq), n);

  const clusterParent: number[] = [-1];
  const clusterBirth: number[] = [0];
  const clusterSize: number[] = [n];
  const clusterChildren: number[][] = [[]];
  const fallCluster = new Int32Array(n);
  const fallLambda = new Float64Array(n);

  const dropSubtree = (root: number, cluster: number, lambda: number) => {
    const stack = [root];
    while (stack.length > 0) {
      const node = stack.pop() as number;
      if (node < n) {
        fallCluster[node] = cluster;
        fallLambda[node] = lambda;
      } else {
        stack.push(tree.left[node - n], tree.right[node - n]);
      }
    }
  };

  const nodes = [2 * n - 2];
  const owners = [0];
  const lambdas = [0];
  while (nodes.length > 0) {
    const node = nodes.pop() as number;
    const cluster = owners.pop() as number;
    const inherited = lambdas.pop() as number;
    if (node < n) {
      fallCluster[node] = cluster;
      fallLambda[node] = inherited;
      continue;
    }
    const lambda = 1 / Math.max(tree.distance[node - n], MIN_DISTANCE);
    const l = tree.left[node - n];
    const r = tree.right[node - n];
    const bigL = tree.size[l] >= minClusterSize;
    const bigR = tree.size[r] >= minClusterSize;
    if (bigL && bigR) {
      for (const child of [l, r]) {
        const id = clusterParent.length;
        clusterParent.push(cluster);
        clusterBirth.push(lambda);
        clusterSize.push(tree.size[child]);
        clusterChildren.push([]);
        clusterChildren[cluster].push(id);
        nodes.push(child);
        owners.push(id);
        lambdas.push(lambda);
      }
    } else if (bigL || bigR) {
      const keep = bigL ? l : r;
      dropSubtree(bigL ? r : l, cluster, lambda);
      nodes.push(keep);
      owners.push(cluster);
      lambdas.push(lambda);
    } else {
      dropSubtree(l, cluster, lambda);
      dropSubtree(r, cluster, lambda);
    }
  }

  const total = clusterParent.length;
  if (total === 1) return new Int32Array(n);

  const stability = new Float64Array(total);
  for (let p = 0; p < n; p++) {
    stability[fallCluster[p]] += fallLambda[p] - clusterBirth[fallCluster[p]];
  }
  for (let c = 1; c < total; c++) {
    const p = clusterParent[c];
    stability[p] += clusterSize[c] * (clusterBirth[c] - clusterBirth[p]);
  }

  const selected = new Uint8Array(total);
  for (let c = total - 1; c >= 1; c--) {
    let childSum = 0;
    for (const child of clusterChildren[c]) childSum += stability[child];
    if (stability[c] >= childSum) {
      selected[c] = 1;
      const stack = [...clusterChildren[c]];
      while (stack.length > 0) {
        const d = stack.pop() as number;
        selected[d] = 0;
        stack.push(...clusterChildren[d]);
      }
    } else {
      stability[c] = childSum;
    }
  }

  const resolved = new Int32Array(total).fill(NOISE_CLUSTER);
  for (let c = 0; c < total; c++) {
    let walk = c;
    while (walk >= 0 && !selected[walk]) walk = clusterParent[walk];
    resolved[c] = walk;
  }

  const labels = new Int32Array(n);
  const counts = new Map<number, { count: number; first: number }>();
  for (let p = 0; p < n; p++) {
    const cluster = resolved[fallCluster[p]];
    labels[p] = cluster;
    if (cluster < 0) continue;
    const entry = counts.get(cluster);
    if (entry) entry.count++;
    else counts.set(cluster, { count: 1, first: p });
  }
  if (counts.size === 0) return new Int32Array(n);

  const ranked = [...counts.entries()].sort(
    (x, y) => y[1].count - x[1].count || x[1].first - y[1].first,
  );
  const rename = new Map<number, number>();
  ranked.forEach(([cluster], rank) => rename.set(cluster, rank));
  for (let p = 0; p < n; p++) {
    if (labels[p] >= 0) labels[p] = rename.get(labels[p]) as number;
  }
  return labels;
}
