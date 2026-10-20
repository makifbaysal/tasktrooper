import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type DataSourceField, type DataSourceKindInfo, type StoreCredentialView } from "@/api";
import { DataSourceForm } from "@/components/operations/sources/DataSourceForm";
import { emptyDraft, optionKey, queryHint, sourceAddress } from "@/components/operations/sources/dataSourceSchema";
import { I18nProvider } from "@/hooks/useI18n";
import { en } from "@/locales/en";
import { dataSource } from "@/test/dataSourceFixtures";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { createDataSource } = vi.hoisted(() => ({ createDataSource: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, createDataSource } };
});

function field(partial: Partial<DataSourceField> & Pick<DataSourceField, "key" | "label" | "type">): DataSourceField {
  return { help: "", required: false, options: [], placeholder: "", show_when: {}, secret: false, ...partial };
}

// As GET /v1/data-source-kinds lists them; labels and help are the server's
// English, which the locale files replace.
const playKind: DataSourceKindInfo = {
  kind: "google_play_vitals",
  label: "server label",
  description: "server description",
  capabilities: ["metrics", "crashes"],
  fields: [
    field({ key: "store_credential", label: "x", type: "store_credential", provider: "google_play", default: "google_play", required: true }),
    field({ key: "package_name", label: "x", type: "text" }),
  ],
};

const sentryKind: DataSourceKindInfo = {
  kind: "sentry",
  label: "Sentry",
  description: "",
  capabilities: ["metrics", "errors", "crashes"],
  fields: [
    field({ key: "url", label: "Sentry URL", type: "url", required: true, default: "https://sentry.io", placeholder: "https://sentry.io" }),
    field({ key: "org", label: "Organization", type: "text", required: true }),
    field({ key: "token", label: "Auth token", type: "secret", required: true, secret: true }),
  ],
};

const datadogKind: DataSourceKindInfo = {
  kind: "datadog",
  label: "Datadog",
  description: "",
  capabilities: ["metrics"],
  fields: [
    field({ key: "site", label: "Site", type: "select", required: true, options: ["datadoghq.com", "datadoghq.eu"], default: "datadoghq.com", placeholder: "datadoghq.com" }),
    field({ key: "api_key", label: "API key", type: "secret", required: true, secret: true }),
    field({ key: "app_key", label: "Application key", type: "secret", required: true, secret: true }),
  ],
};

const ga4Kind: DataSourceKindInfo = {
  kind: "ga4",
  label: "Google Analytics 4",
  description: "",
  capabilities: ["metrics"],
  fields: [
    field({ key: "property_id", label: "Property ID", type: "text", required: true }),
    field({ key: "auth", label: "Sign in with", type: "select", options: ["cloud_account", "service_account"], default: "cloud_account", placeholder: "cloud_account" }),
    field({ key: "cloud_account_id", label: "Google Cloud account", type: "cloud_account", provider: "gcp", required: true, show_when: { auth: "cloud_account" } }),
    field({ key: "service_account_json", label: "Service account key (JSON)", type: "secret", required: true, secret: true, show_when: { auth: "service_account" }, multiline: true }),
  ],
};

function renderForm(kind: DataSourceKindInfo, storeCredentials: StoreCredentialView[] | null = null) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <DataSourceForm
          open
          onOpenChange={() => {}}
          kinds={[kind]}
          onSaved={vi.fn()}
          storeCredentials={storeCredentials}
          initial={{ kind: kind.kind, values: {} }}
        />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  createDataSource.mockReset();
});

describe("store credential fields", () => {
  it("offers the workspace's saved credential, picked by its default, and saves the provider", async () => {
    createDataSource.mockResolvedValue(dataSource({ kind: "google_play_vitals" }));
    renderForm(playKind, [{ provider: "google_play", configured: true, updated_at: "" }]);
    expect(screen.getByText("Kind: Google Play vitals")).toBeInTheDocument();
    expect(screen.getByLabelText("Google Play credential")).toHaveTextContent("Google Play service account");
    expect(screen.getByText(/needs the Play Developer Reporting API enabled/)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Package name/)).toHaveAttribute("placeholder", "com.example.app");
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute("placeholder", "Defaults to the kind's name");
    fireEvent.change(screen.getByLabelText(/^Package name/), { target: { value: "com.acme.app" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() =>
      expect(createDataSource).toHaveBeenCalledWith({
        kind: "google_play_vitals",
        config: { store_credential: "google_play", package_name: "com.acme.app" },
        secrets: {},
      }),
    );
  });

  it("points to Settings → Integrations when the credential isn't saved", () => {
    renderForm(playKind, [{ provider: "google_play", configured: false, updated_at: "" }]);
    expect(screen.getByText("No Google Play service account is saved yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Save it in Settings → Integrations" })).toHaveAttribute("href", "/settings/integrations");
  });

  it("words the server's refusal of a credential that isn't saved", async () => {
    createDataSource.mockRejectedValue(new ApiError("bad", 400, "", { store_credential: "not_found" }));
    renderForm(playKind, null);
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(await screen.findByText("That credential isn't saved yet. Save it in Settings → Integrations first.")).toBeInTheDocument();
  });
});

describe("the new kinds' fields", () => {
  it("prefills a field's default: Sentry's URL", () => {
    renderForm(sentryKind);
    expect(screen.getByLabelText("Sentry URL")).toHaveValue("https://sentry.io");
    expect(screen.getByText(/org:read, project:read and event:read/)).toBeInTheDocument();
    expect(screen.getByLabelText("Auth token")).toHaveAttribute("type", "password");
  });

  it("picks Datadog's default site and lists the others by their locale labels, though their keys hold dots", () => {
    renderForm(datadogKind);
    expect(screen.getByLabelText("Site")).toHaveTextContent("US1 · datadoghq.com");
    expect(screen.getByText(/app.datadoghq.eu is datadoghq.eu/)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Site"));
    expect(screen.getByRole("option", { name: "EU1 · datadoghq.eu" })).toBeInTheDocument();
    expect(optionKey("ddog-gov.com")).toBe("ddog-gov_com");
  });

  it("asks GA4 for a connected account or a key file typed in a text area", () => {
    renderForm(ga4Kind);
    expect(screen.getByLabelText(/^Sign in with/)).toHaveTextContent("A connected Google Cloud account");
    expect(screen.getByText("No Google Cloud account is connected yet.")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/^Sign in with/));
    fireEvent.click(screen.getByRole("option", { name: "A service account key" }));
    expect(screen.getByLabelText("Service account key (JSON)").tagName).toBe("TEXTAREA");
  });

  it("starts a new source from each field's default, an edit from what is stored", () => {
    expect(emptyDraft(datadogKind).values.site).toBe("datadoghq.com");
    expect(emptyDraft(datadogKind, dataSource({ kind: "datadog", config: { site: "datadoghq.eu" } })).values.site).toBe("datadoghq.eu");
    expect(emptyDraft(playKind).values.store_credential).toBe("google_play");
  });

  it("lists a store source by the app it reads", () => {
    expect(sourceAddress(dataSource({ kind: "google_play_vitals", config: { store_credential: "google_play", package_name: "com.acme" } }), playKind)).toBe("com.acme");
  });

  it("has a query hint for every F4 kind", () => {
    const t = (key: string) => {
      const value = key.split(".").reduce<unknown>((o, k) => (o as Record<string, unknown> | undefined)?.[k], en);
      return typeof value === "string" ? value : key;
    };
    for (const kind of ["google_play_vitals", "app_store_metrics", "sentry", "loki", "elasticsearch", "tempo", "posthog", "ga4", "plausible", "vercel", "datadog", "newrelic"]) {
      expect(queryHint(t, kind), kind).not.toBe("");
    }
    expect(queryHint(t, "tempo")).toMatch(/TraceQL/);
    expect(queryHint(t, "newrelic")).toMatch(/NRQL/);
    expect(queryHint(t, "loki")).toMatch(/LogQL/);
  });
});
