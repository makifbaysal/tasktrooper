import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type ObservabilityReadiness } from "@/api";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";

const apiMock = vi.hoisted(() => ({
  getComponentObservability: vi.fn(),
  enableComponentObservability: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { EnableObservabilityButton } from "@/components/operations/observability/EnableObservabilityButton";
import { ObservabilityReadinessCard } from "@/components/operations/observability/ObservabilityReadinessCard";

const entity: OpsEntity = {
  id: "c-api",
  name: "api",
  role: "backend",
  repositoryId: "r-shop",
  repositoryName: "shop",
  projectIds: [],
  environments: [],
};

const readiness: ObservabilityReadiness = {
  component_id: "c-api",
  environment: "production",
  role: "backend",
  provider: "gcp",
  signals: [
    { signal: "http.requests", status: "bound", golden: true },
    { signal: "http.latency", status: "unbound", golden: true },
    { signal: "http.error_ratio", status: "unbound", golden: true },
  ],
  instrumentation: { otel: false, sentry: true, prometheus: false },
  suggestions: [
    { goal: "metrics", code: "golden_partly_unbound" },
    { goal: "traces", code: "no_tracing_sdk" },
  ],
};

function renderButton(target: OpsEntity = entity) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <EnableObservabilityButton entity={target} env="production" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

async function openDialog() {
  fireEvent.click(screen.getByRole("button", { name: "Enable observability for api" }));
  return screen.findByRole("dialog", { name: "Enable observability for api" });
}

beforeEach(() => {
  sessionStorage.clear();
  writeCache("operations.observability.c-api.production", undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  apiMock.getComponentObservability.mockResolvedValue(readiness);
});

describe("EnableObservabilityDialog", () => {
  it("summarises what the component reports, in words, and ticks the goals its gaps name", async () => {
    renderButton();
    const dialog = await openDialog();
    expect(await within(dialog).findByText("Golden signals bound: 1 of 3")).toBeInTheDocument();
    expect(within(dialog).getByText("Metrics read from Google Cloud")).toBeInTheDocument();
    expect(within(dialog).getByText("Sentry")).toBeInTheDocument();
    expect(within(dialog).getByText("Some of its golden signals aren't bound to a data source.")).toBeInTheDocument();
    expect(within(dialog).getByText("No tracing SDK (OpenTelemetry, Datadog or New Relic) is in its code.")).toBeInTheDocument();
    expect(apiMock.getComponentObservability).toHaveBeenCalledWith("c-api", "production");

    expect(within(dialog).getByRole("checkbox", { name: "Metrics" })).toBeChecked();
    expect(within(dialog).getByRole("checkbox", { name: "Traces" })).toBeChecked();
    expect(within(dialog).getByRole("checkbox", { name: "Logs" })).not.toBeChecked();
    expect(within(dialog).queryByRole("checkbox", { name: "Real-user monitoring" })).not.toBeInTheDocument();
    expect(within(dialog).getByRole("radio", { name: /The cloud it runs on/ })).toHaveAttribute("aria-checked", "true");
  });

  it("asks for the OTLP endpoint's URL, then creates the task and links to it", async () => {
    apiMock.enableComponentObservability.mockResolvedValue({ task_id: "t-9", task_key: "SHOP-9" });
    renderButton();
    const dialog = await openDialog();
    await within(dialog).findByText("Golden signals bound: 1 of 3");
    fireEvent.click(within(dialog).getByRole("radio", { name: /An OTLP endpoint/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    expect(within(dialog).getByText("Enter the endpoint's URL.")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("OTLP endpoint URL"), { target: { value: "ftp://collector" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    expect(within(dialog).getByText("Enter an http or https URL.")).toBeInTheDocument();
    expect(apiMock.enableComponentObservability).not.toHaveBeenCalled();

    fireEvent.change(within(dialog).getByLabelText("OTLP endpoint URL"), { target: { value: "https://otel.example.com:4318" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Logs" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    await waitFor(() =>
      expect(apiMock.enableComponentObservability).toHaveBeenCalledWith("c-api", {
        environment: "production",
        goals: ["metrics", "traces", "logs"],
        target: "otlp_endpoint",
        otlp_endpoint: "https://otel.example.com:4318",
      }),
    );
    const done = await screen.findByRole("dialog", { name: "Enable observability for api" });
    expect(within(done).getByText("Task SHOP-9 created")).toBeInTheDocument();
    expect(within(done).getByRole("link", { name: "Open SHOP-9" })).toHaveAttribute("href", "/board?task=t-9");
  });

  it("asks for at least one goal, and says how to turn the release engineer on when there is none", async () => {
    apiMock.enableComponentObservability.mockRejectedValue(new ApiError("no release engineer", 409, "no_release_engineer"));
    renderButton();
    const dialog = await openDialog();
    await within(dialog).findByText("Golden signals bound: 1 of 3");
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Metrics" }));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Traces" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    expect(within(dialog).getByText("Pick at least one thing to set up.")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Logs" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    expect(await within(dialog).findByText("No release engineer is on for this component")).toBeInTheDocument();
  });

  it("shows the server's field error under its field", async () => {
    apiMock.enableComponentObservability.mockRejectedValue(new ApiError("goals: invalid_option", 400, "", { goals: "invalid_option" }));
    renderButton();
    const dialog = await openDialog();
    await within(dialog).findByText("Golden signals bound: 1 of 3");
    fireEvent.click(within(dialog).getByRole("button", { name: "Create the task" }));
    expect(await within(dialog).findByText("One of these goals isn't known to the server.")).toBeInTheDocument();
  });

  it("is not offered for a component that does not run anywhere", () => {
    renderButton({ ...entity, role: "library" });
    expect(screen.queryByRole("button", { name: /Enable observability/ })).not.toBeInTheDocument();
  });
});

describe("ObservabilityReadinessCard", () => {
  function renderCard() {
    return render(
      <MemoryRouter>
        <I18nProvider>
          <ObservabilityReadinessCard entity={entity} env="production" />
        </I18nProvider>
      </MemoryRouter>,
    );
  }

  it("shows the gaps, at most two, with the way to close them", async () => {
    apiMock.getComponentObservability.mockResolvedValue({
      ...readiness,
      suggestions: [...(readiness.suggestions ?? []), { goal: "logs", code: "no_log_source" }],
    });
    renderCard();
    const card = await screen.findByRole("region", { name: "Observability gaps" });
    expect(within(card).getByText("Some of its golden signals aren't bound to a data source.")).toBeInTheDocument();
    expect(within(card).getByText("and 1 more")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Enable observability for api" })).toBeInTheDocument();
  });

  it("shows nothing without a gap or when the read fails", async () => {
    apiMock.getComponentObservability.mockResolvedValue({ ...readiness, suggestions: [] });
    const { unmount } = renderCard();
    await waitFor(() => expect(apiMock.getComponentObservability).toHaveBeenCalled());
    expect(screen.queryByRole("region", { name: "Observability gaps" })).not.toBeInTheDocument();
    unmount();

    sessionStorage.clear();
    writeCache("operations.observability.c-api.production", undefined);
    apiMock.getComponentObservability.mockRejectedValue(new ApiError("not found", 404));
    renderCard();
    await waitFor(() => expect(apiMock.getComponentObservability).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("region", { name: "Observability gaps" })).not.toBeInTheDocument();
  });
});
