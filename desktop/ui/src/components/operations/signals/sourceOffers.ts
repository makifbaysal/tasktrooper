import type { DataSourceKindInfo, DataSourceOffer, SaveSignalBindingRequest, StoreCredentialView } from "@/api";

/** One offer apart from another: the same kind, recipe and params is one offer. */
export function offerKey(offer: DataSourceOffer): string {
  const params = Object.entries(offer.params ?? {})
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([k, v]) => `${k}=${v}`)
    .join("&");
  return `${offer.kind}:${offer.recipe_id}:${params}`;
}

/** The offers of an answer, each once, in the server's order. */
export function uniqueOffers(offers: DataSourceOffer[] | null | undefined): DataSourceOffer[] {
  const seen = new Set<string>();
  const out: DataSourceOffer[] = [];
  for (const offer of offers ?? []) {
    const key = offerKey(offer);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(offer);
  }
  return out;
}

/** The store credential provider an offer's source reads, when it reads one. */
export function offerCredential(offer: DataSourceOffer, kind?: DataSourceKindInfo): string | undefined {
  if (offer.store_credential) return offer.store_credential;
  const field = kind?.fields.find((f) => f.type === "store_credential");
  return field ? offer.config?.[field.key] || field.default || field.provider || undefined : undefined;
}

/** Whether the credential the offer needs is saved: undefined while the
 * vault is unknown, true for an offer that needs none. */
export function credentialReady(
  offer: DataSourceOffer,
  credentials: StoreCredentialView[] | null | undefined,
  kind?: DataSourceKindInfo,
): boolean | undefined {
  const provider = offerCredential(offer, kind);
  if (!provider) return true;
  if (!credentials) return undefined;
  const wanted = provider === "app_store_connect" ? "asc" : provider;
  return credentials.some((c) => c.provider === wanted && c.configured);
}

/** What the add dialog opens on: the offer's kind and config, its store
 * credential field filled in when the config left it out. */
export function offerInitial(offer: DataSourceOffer, kind?: DataSourceKindInfo): { kind: string; values: Record<string, string> } {
  const values = { ...(offer.config ?? {}) };
  const field = kind?.fields.find((f) => f.type === "store_credential");
  const provider = offerCredential(offer, kind);
  if (field && provider && !values[field.key]) values[field.key] = provider;
  return { kind: offer.kind, values };
}

/** The bindings to put in place once the offered source exists: the offered
 * recipe, with its params, for every signal it was offered for. */
export function offerBindings(offer: DataSourceOffer, dataSourceId: string): Array<{ signal: string; body: SaveSignalBindingRequest }> {
  return [...new Set(offer.signals ?? [])].map((signal) => ({
    signal,
    body: { data_source_id: dataSourceId, recipe_id: offer.recipe_id, params: { ...(offer.params ?? {}) } },
  }));
}

/** The words an offer card leads with: a store kind's own, else the generic one. */
export function offerTitleKey(kind: string): string {
  switch (kind) {
    case "google_play_vitals":
      return "playVitals";
    case "app_store_metrics":
      return "appStoreMetrics";
    default:
      return "generic";
  }
}
