export interface EmbeddingMapSearch {
  repositoryId: string;
  query: string;
  hits: { id: string; path: string; symbol?: string }[];
}

export interface PlacedMarker {
  hitId: string;
  rank: number;
  x: number;
  y: number;
}

const RING_STEP = 0.006;

export function placeMarkers(
  hits: { id: string }[],
  anchorIndexByHitId: Map<string, number>,
  positions: Float32Array,
): PlacedMarker[] {
  const perAnchor = new Map<number, number>();
  const markers: PlacedMarker[] = [];
  hits.forEach((hit, i) => {
    const anchor = anchorIndexByHitId.get(hit.id);
    if (anchor === undefined || anchor * 2 + 1 >= positions.length) return;
    const k = perAnchor.get(anchor) ?? 0;
    perAnchor.set(anchor, k + 1);
    let x = positions[anchor * 2];
    let y = positions[anchor * 2 + 1];
    if (k > 0) {
      const angle = (k - 1) * 2.399963229728653;
      x += Math.cos(angle) * RING_STEP * k;
      y += Math.sin(angle) * RING_STEP * k;
    }
    markers.push({ hitId: hit.id, rank: i + 1, x, y });
  });
  return markers;
}

export function groupMembers(groupIds: string[], groupId: string): number[] {
  const members: number[] = [];
  for (let i = 0; i < groupIds.length; i++) if (groupIds[i] === groupId) members.push(i);
  return members;
}

export function topFiles(
  paths: string[],
  members: number[],
  limit = 5,
): { path: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const index of members) {
    const path = paths[index];
    if (path === undefined) continue;
    counts.set(path, (counts.get(path) ?? 0) + 1);
  }
  return [...counts]
    .map(([path, count]) => ({ path, count }))
    .sort((a, b) => b.count - a.count || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
    .slice(0, limit);
}

export function nearestMembers(positions: Float32Array, members: number[], limit = 3): number[] {
  if (members.length === 0) return [];
  let mx = 0;
  let my = 0;
  for (const i of members) {
    mx += positions[i * 2];
    my += positions[i * 2 + 1];
  }
  mx /= members.length;
  my /= members.length;
  return members
    .map((i) => ({ i, d: (positions[i * 2] - mx) ** 2 + (positions[i * 2 + 1] - my) ** 2 }))
    .sort((a, b) => a.d - b.d || a.i - b.i)
    .slice(0, limit)
    .map((entry) => entry.i);
}
