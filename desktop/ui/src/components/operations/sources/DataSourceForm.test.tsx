import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type DataSource, type DataSourceKindInfo } from "@/api";
import { DataSourceForm } from "@/components/operations/sources/DataSourceForm";
import { I18nProvider } from "@/hooks/useI18n";
import { cloudAccount, dataSource, gcpMonitoringKind, lokiKind, prometheusKind } from "@/test/dataSourceFixtures";
import { MemoryRouter } from "react-router-dom";

// jsdom has no layout engine; Radix Select scrolls the open item into view.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { createDataSource, updateDataSource, getDataSource } = vi.hoisted(() => ({
  createDataSource: vi.fn(),
  updateDataSource: vi.fn(),
  getDataSource: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, createDataSource, updateDataSource, getDataSource } };
});

function formElement(kinds: DataSourceKindInfo[], source: DataSource | undefined, onSaved: (ds: DataSource) => void) {
  return (
    <I18nProvider>
      <DataSourceForm open onOpenChange={() => {}} kinds={kinds} source={source} onSaved={onSaved} />
    </I18nProvider>
  );
}

function renderForm(props: { kinds?: DataSourceKindInfo[]; source?: DataSource; onSaved?: (ds: DataSource) => void } = {}) {
  const onSaved = props.onSaved ?? vi.fn();
  const view = render(formElement(props.kinds ?? [prometheusKind], props.source, onSaved));
  return { onSaved, rerender: (kinds: DataSourceKindInfo[]) => view.rerender(formElement(kinds, props.source, onSaved)) };
}

function chooseAuth(option: RegExp) {
  fireEvent.click(screen.getByLabelText(/Authentication/));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

beforeEach(() => {
  createDataSource.mockReset();
  updateDataSource.mockReset();
  getDataSource.mockReset();
});

describe("DataSourceForm — add", () => {
  it("picks a kind, then draws the form from its fields with show_when", async () => {
    renderForm();
    const picker = screen.getByRole("radiogroup", { name: "What kind of source is it?" });
    expect(within(picker).getByRole("radio", { name: /Prometheus compatible/ })).toHaveAttribute("aria-checked", "true");
    expect(within(picker).getByText("Metrics")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(screen.getByLabelText("URL")).toHaveAttribute("placeholder", "http://localhost:9090");
    expect(screen.getByText(/Mimir serves it under \/prometheus/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Bearer token")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();

    chooseAuth(/^Bearer token$/);
    expect(await screen.findByLabelText("Bearer token")).toHaveAttribute("type", "password");
    chooseAuth(/^Basic/);
    expect(await screen.findByLabelText("Username")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.queryByLabelText("Bearer token")).not.toBeInTheDocument();
  });

  it("checks required fields before sending anything", () => {
    renderForm();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    chooseAuth(/^Bearer token$/);
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(screen.getAllByText("Required.")).toHaveLength(2);
    expect(screen.getByLabelText("URL")).toHaveAttribute("aria-invalid", "true");
    expect(createDataSource).not.toHaveBeenCalled();
  });

  it("sends secrets and headers apart from config, and shows a failed check as saved with an error", async () => {
    const saved = dataSource({ status: "error", status_detail: "dial tcp 127.0.0.1:9090: connection refused", info: { version: "", detail: "" } });
    createDataSource.mockResolvedValue(saved);
    const { onSaved } = renderForm();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "http://localhost:9090" } });
    chooseAuth(/^Bearer token$/);
    fireEvent.change(await screen.findByLabelText("Bearer token"), { target: { value: "s3cret" } });
    fireEvent.click(screen.getByRole("button", { name: "Add header" }));
    fireEvent.change(screen.getByLabelText("Header 1 name"), { target: { value: "X-Scope-OrgID" } });
    const headerValue = screen.getByLabelText("Header 1 value");
    expect(headerValue).toHaveAttribute("type", "password");
    const reveal = screen.getByRole("button", { name: "Show header 1 value" });
    expect(reveal).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(reveal);
    expect(headerValue).toHaveAttribute("type", "text");
    expect(reveal).toHaveAttribute("aria-pressed", "true");
    fireEvent.change(headerValue, { target: { value: "tenant-1" } });
    expect(screen.getByLabelText("Bearer token")).toHaveAttribute("autocomplete", "new-password");
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));

    await waitFor(() => expect(createDataSource).toHaveBeenCalledTimes(1));
    expect(createDataSource).toHaveBeenCalledWith({
      kind: "prometheus",
      config: { url: "http://localhost:9090", auth: "bearer" },
      secrets: { token: "s3cret", headers: "X-Scope-OrgID: tenant-1" },
    });
    expect(await screen.findByText("Saved, but the connection check failed")).toBeInTheDocument();
    expect(screen.getByText("dial tcp 127.0.0.1:9090: connection refused")).toBeInTheDocument();
    expect(onSaved).toHaveBeenCalledWith(saved);
    expect(screen.getByRole("button", { name: "Edit settings" })).toBeInTheDocument();
  });

  it("shows the server's field errors next to their fields", async () => {
    createDataSource.mockRejectedValue(
      new ApiError("invalid data source: url: not_permitted", 400, "", { url: "not_permitted" }),
    );
    renderForm();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "http://169.254.169.254" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(await screen.findByText(/This address isn't allowed/)).toBeInTheDocument();
    expect(screen.getByLabelText("URL")).toHaveAttribute("aria-invalid", "true");
  });

  it("renders a kind the locale files do not know with the server's own labels", () => {
    renderForm({ kinds: [prometheusKind, lokiKind] });
    fireEvent.click(screen.getByRole("radio", { name: /Loki \(server label\)/ }));
    expect(screen.getByText("Server-side description of Loki.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByLabelText("Endpoint (server label)")).toBeInTheDocument();
    expect(screen.getByText("Server help for the endpoint.")).toBeInTheDocument();
  });
});

describe("DataSourceForm — edit", () => {
  const source = dataSource();

  it("keeps a saved secret left empty and sends \"\" only for one the user removes", async () => {
    updateDataSource.mockResolvedValue(source);
    renderForm({ source });
    expect(screen.getByLabelText("Bearer token")).toHaveValue("");
    expect(screen.getByText("Saved. Leave it empty to keep it.")).toBeInTheDocument();
    expect(screen.getByText(/Saved headers are kept/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Remove the saved Extra headers" }));
    expect(screen.getByText("Will be removed when you save.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));

    await waitFor(() => expect(updateDataSource).toHaveBeenCalledTimes(1));
    expect(updateDataSource).toHaveBeenCalledWith("ds-1", {
      config: {},
      secrets: { headers: "" },
    });
    expect(await screen.findByText("TaskTrooper reached Prod Prometheus, version 2.53.0.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
  });

  it("asks for every saved credential again when the address moves to another host", async () => {
    updateDataSource.mockResolvedValue(dataSource({ config: { url: "https://mimir.example.com", auth: "bearer" } }));
    renderForm({ source });
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "https://mimir.example.com" } });
    expect(screen.getByText("Saved credentials have to be entered again")).toBeInTheDocument();
    expect(screen.queryByText("Saved. Leave it empty to keep it.")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(screen.getAllByText(/Saved credentials aren't carried to a new address/)).toHaveLength(2);
    expect(updateDataSource).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Bearer token"), { target: { value: "fresh" } });
    fireEvent.click(screen.getByRole("button", { name: "Remove the saved Extra headers" }));
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(updateDataSource).toHaveBeenCalledTimes(1));
    expect(updateDataSource).toHaveBeenCalledWith("ds-1", {
      config: { url: "https://mimir.example.com" },
      secrets: { token: "fresh", headers: "" },
    });
  });

  it("waits for the kind before drawing an edit, so a save never sends a blank draft", async () => {
    updateDataSource.mockResolvedValue(source);
    const { rerender } = renderForm({ kinds: [], source });
    expect(screen.queryByLabelText("URL")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save and check" })).toBeDisabled();

    rerender([prometheusKind]);
    expect(await screen.findByLabelText("URL")).toHaveValue("https://prom.example.com");
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(updateDataSource).toHaveBeenCalledTimes(1));
    expect(updateDataSource.mock.calls[0][1].config).toEqual({});
  });

  it("keeps the other field errors that came with secrets_required", async () => {
    updateDataSource.mockRejectedValueOnce(
      new ApiError("invalid data source", 400, "", { secrets: "secrets_required", url: "not_permitted" }),
    );
    renderForm({ source });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(await screen.findByText(/This address isn't allowed/)).toBeInTheDocument();
    expect(screen.getAllByText(/Saved credentials aren't carried to a new address/)).toHaveLength(2);
  });

  it("reads the source again when the server knows of a saved secret this snapshot does not", async () => {
    const stale = dataSource({ config: { url: "https://prom.example.com", auth: "none" }, secrets_set: [] });
    updateDataSource.mockRejectedValueOnce(
      new ApiError("invalid data source", 400, "", { secrets: "secrets_required" }),
    );
    getDataSource.mockResolvedValue({ ...stale, secrets_set: ["headers"] });
    renderForm({ source: stale });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(getDataSource).toHaveBeenCalledWith("ds-1"));
    expect(await screen.findByText(/Saved credentials aren't carried to a new address/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove the saved Extra headers" })).toBeInTheDocument();
  });

  it("says so instead of looping when nothing it knows of needs entering again", async () => {
    const bare = dataSource({ config: { url: "https://prom.example.com", auth: "none" }, secrets_set: [] });
    updateDataSource.mockRejectedValueOnce(
      new ApiError("invalid data source", 400, "", { secrets: "secrets_required" }),
    );
    getDataSource.mockResolvedValue(bare);
    renderForm({ source: bare });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(await screen.findByText("Couldn't save the data source")).toBeInTheDocument();
    expect(screen.getByText(/Saved credentials aren't carried to a new address/)).toBeInTheDocument();
  });

  it("switches to re-entry when the server answers secrets_required", async () => {
    updateDataSource.mockRejectedValueOnce(
      new ApiError("invalid data source: secrets: secrets_required", 400, "", { secrets: "secrets_required" }),
    );
    renderForm({ source });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(await screen.findByText("Saved credentials have to be entered again")).toBeInTheDocument();
    expect(screen.getAllByText(/Saved credentials aren't carried to a new address/)).toHaveLength(2);
  });
});

describe("DataSourceForm — refused saves", () => {
  const source = dataSource();

  it("moves focus to the first field that needs attention", async () => {
    renderForm({ source });
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(screen.getByLabelText("URL")).toHaveFocus());
    expect(updateDataSource).not.toHaveBeenCalled();
  });

  it("hands a source deleted elsewhere back to the page", async () => {
    updateDataSource.mockRejectedValueOnce(new ApiError("data source not found", 404));
    const onGone = vi.fn();
    render(
      <I18nProvider>
        <DataSourceForm open onOpenChange={() => {}} kinds={[prometheusKind]} source={source} onSaved={vi.fn()} onGone={onGone} />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(onGone).toHaveBeenCalledWith(source));
  });
});

describe("DataSourceForm — a kind that reads through a cloud account", () => {
  const gcpAccount = cloudAccount();
  const awsAccount = cloudAccount({ id: "acc-aws", provider: "aws", label: "Acme AWS", meta: { account_id: "1234" } });

  function renderCloud(props: Partial<React.ComponentProps<typeof DataSourceForm>> = {}) {
    const onSaved = vi.fn();
    render(
      <MemoryRouter>
        <I18nProvider>
          <DataSourceForm
            open
            onOpenChange={() => {}}
            kinds={[prometheusKind, gcpMonitoringKind]}
            onSaved={onSaved}
            cloudAccounts={[gcpAccount, awsAccount]}
            {...props}
          />
        </I18nProvider>
      </MemoryRouter>,
    );
    return { onSaved };
  }

  it("picks the account from the connected ones of the field's provider only, and saves its id", async () => {
    createDataSource.mockResolvedValue(dataSource({ id: "ds-gcp", kind: "gcp_monitoring", config: { cloud_account_id: "acc-gcp" } }));
    renderCloud();
    fireEvent.click(screen.getByRole("radio", { name: /Google Cloud Monitoring/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByText(/Monitoring Viewer role \(roles\/monitoring.viewer\)/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Project/)).toHaveAttribute("placeholder", "The account's project");

    fireEvent.click(screen.getByLabelText("Google Cloud account"));
    const options = screen.getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Acme productionacme-prod"]);
    fireEvent.click(options[0]);
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() =>
      expect(createDataSource).toHaveBeenCalledWith({ kind: "gcp_monitoring", config: { cloud_account_id: "acc-gcp" }, secrets: {} }),
    );
  });

  it("says how to connect an account when the provider has none, and asks for one before saving", () => {
    renderCloud({ cloudAccounts: [awsAccount] });
    fireEvent.click(screen.getByRole("radio", { name: /Google Cloud Monitoring/ }));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByText("No Google Cloud account is connected yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect one in Settings → Integrations" })).toHaveAttribute("href", "/settings/integrations");
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(screen.getByText("Required.")).toBeInTheDocument();
    expect(createDataSource).not.toHaveBeenCalled();
  });

  it("words the server's refusals of an account", async () => {
    createDataSource
      .mockRejectedValueOnce(new ApiError("invalid", 400, "", { cloud_account_id: "wrong_provider" }))
      .mockRejectedValueOnce(new ApiError("invalid", 400, "", { cloud_account_id: "not_found" }))
      .mockRejectedValueOnce(new ApiError("invalid", 400, "", { cloud_account_id: "not_permitted" }));
    renderCloud({ initial: { kind: "gcp_monitoring", values: { cloud_account_id: "acc-gcp" } } });
    const save = () => fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    save();
    expect(await screen.findByText("That account belongs to another cloud provider.")).toBeInTheDocument();
    save();
    expect(await screen.findByText("That account is no longer connected.")).toBeInTheDocument();
    save();
    expect(await screen.findByText("Cloud accounts can't be used for data sources in this setup.")).toBeInTheDocument();
  });

  it("opens past the kind step with the offered kind and account filled in", () => {
    renderCloud({ initial: { kind: "gcp_monitoring", values: { cloud_account_id: "acc-gcp" } } });
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    expect(screen.getByText("Kind: Google Cloud Monitoring")).toBeInTheDocument();
    expect(screen.getByLabelText("Google Cloud account")).toHaveTextContent("Acme production");
  });
});
