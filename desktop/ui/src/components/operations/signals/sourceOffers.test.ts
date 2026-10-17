import { describe, expect, it } from "vitest";
import type { DataSourceKindInfo, DataSourceOffer, StoreCredentialView } from "@/api";
import { bindableSources, canPreview, signalKindOf } from "@/components/operations/signals/signalBinding";
import { credentialReady, offerBindings, offerInitial, offerKey, offerTitleKey, uniqueOffers } from "@/components/operations/signals/sourceOffers";
import { dataSource } from "@/test/dataSourceFixtures";

const playKind: DataSourceKindInfo = {
  kind: "google_play_vitals",
  label: "Google Play vitals",
  description: "",
  capabilities: ["metrics", "crashes"],
  fields: [
    {
      key: "store_credential",
      label: "Google Play credential",
      help: "",
      type: "store_credential",
      required: true,
      options: [],
      placeholder: "",
      show_when: {},
      secret: false,
      provider: "google_play",
      default: "google_play",
    },
    { key: "package_name", label: "Package name", help: "", type: "text", required: false, options: [], placeholder: "", show_when: {}, secret: false },
  ],
};

const offer: DataSourceOffer = {
  kind: "google_play_vitals",
  config: { package_name: "com.acme.app" },
  recipe_id: "play-vitals",
  params: { package: "com.acme.app" },
  signals: ["mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate", "mobile.crash_rate"],
  store_credential: "google_play",
};

const vault = (configured: boolean): StoreCredentialView[] => [
  { provider: "google_play", configured, updated_at: "" },
  { provider: "asc", configured: false, updated_at: "" },
];

describe("source offers", () => {
  it("opens the add dialog on the offer's kind and config, its credential filled in", () => {
    expect(offerInitial(offer, playKind)).toEqual({
      kind: "google_play_vitals",
      values: { package_name: "com.acme.app", store_credential: "google_play" },
    });
    expect(offerInitial({ ...offer, store_credential: undefined }, playKind).values.store_credential).toBe("google_play");
  });

  it("says whether the credential the offer needs is saved", () => {
    expect(credentialReady(offer, vault(true), playKind)).toBe(true);
    expect(credentialReady(offer, vault(false), playKind)).toBe(false);
    expect(credentialReady(offer, null, playKind)).toBeUndefined();
    expect(credentialReady({ ...offer, store_credential: "app_store_connect" }, [{ provider: "asc", configured: true, updated_at: "" }])).toBe(true);
    expect(credentialReady({ ...offer, kind: "sentry", store_credential: undefined }, null)).toBe(true);
  });

  it("binds the offered recipe for each offered signal once, through the new source", () => {
    expect(offerBindings(offer, "ds-new")).toEqual(
      ["mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate"].map((signal) => ({
        signal,
        body: { data_source_id: "ds-new", recipe_id: "play-vitals", params: { package: "com.acme.app" } },
      })),
    );
  });

  it("lists an offer once and words it by kind", () => {
    expect(uniqueOffers([offer, { ...offer }, { ...offer, params: { package: "com.other" } }])).toHaveLength(2);
    expect(offerKey(offer)).toBe("google_play_vitals:play-vitals:package=com.acme.app");
    expect(offerTitleKey("app_store_metrics")).toBe("appStoreMetrics");
    expect(offerTitleKey("posthog")).toBe("generic");
  });
});

describe("binding sources by signal kind", () => {
  const prom = dataSource({ id: "prom", capabilities: ["metrics"] });
  const tempo = dataSource({ id: "tempo", kind: "tempo", capabilities: ["traces"] });
  const loki = dataSource({ id: "loki", kind: "loki", capabilities: ["logs", "metrics"] });
  const sentry = dataSource({ id: "sentry", kind: "sentry", capabilities: ["metrics", "errors", "crashes"] });

  it("offers only the sources that can feed the signal's kind", () => {
    const all = [prom, tempo, loki, sentry];
    expect(bindableSources(all, "metric").map((s) => s.id)).toEqual(["prom", "loki", "sentry"]);
    expect(bindableSources(all, "trace").map((s) => s.id)).toEqual(["tempo"]);
    expect(bindableSources(all, "log").map((s) => s.id)).toEqual(["loki"]);
    expect(bindableSources(all, "error").map((s) => s.id)).toEqual(["sentry"]);
    const datadog = dataSource({ id: "dd", kind: "datadog", capabilities: ["metrics"] });
    const newrelic = dataSource({ id: "nr", kind: "newrelic", capabilities: ["metrics", "rum", "crashes"] });
    const appStore = dataSource({ id: "asc", kind: "app_store_metrics", capabilities: ["metrics", "crashes"] });
    expect(bindableSources([tempo, datadog, newrelic], "trace").map((s) => s.id)).toEqual(["tempo"]);
    expect(bindableSources([tempo, datadog, newrelic, appStore], "metric").map((s) => s.id)).toEqual(["dd", "nr", "asc"]);
    expect(bindableSources([datadog, newrelic, appStore, sentry], "error").map((s) => s.id)).toEqual(["sentry"]);
  });

  it("previews metrics only, and knows the non-metric signals before the catalog is read", () => {
    expect(canPreview("metric")).toBe(true);
    expect(canPreview("log")).toBe(false);
    expect(canPreview("trace")).toBe(false);
    expect(canPreview("error")).toBe(false);
    expect(signalKindOf("traces.spans")).toBe("trace");
    expect(signalKindOf("errors.issues")).toBe("error");
    expect(signalKindOf("http.requests")).toBe("metric");
    expect(signalKindOf("logs.lines", "log")).toBe("log");
  });
});
