import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DataSourceField, DataSourceKindInfo } from "@/api";
import { inventoryQuery, isMultilineField, queryHint, readsLiveOnly } from "@/components/operations/sources/dataSourceSchema";
import { I18nProvider } from "@/hooks/useI18n";
import { dataSource, prometheusKind } from "@/test/dataSourceFixtures";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

vi.mock("uplot", () => ({
  default: class {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

const { createDataSource, queryDataSource, updateDataSource } = vi.hoisted(() => ({
  createDataSource: vi.fn(),
  queryDataSource: vi.fn(),
  updateDataSource: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, createDataSource, queryDataSource, updateDataSource } };
});

import { DataSourceForm } from "@/components/operations/sources/DataSourceForm";
import { QueryPlayground } from "@/components/operations/sources/QueryPlayground";

function field(partial: Partial<DataSourceField> & Pick<DataSourceField, "key" | "label" | "type">): DataSourceField {
  return { help: "", required: false, options: [], placeholder: "", show_when: {}, secret: false, ...partial };
}

/** The kafka kind as GET /v1/data-source-kinds lists it (server labels, English). */
const kafkaKind: DataSourceKindInfo = {
  kind: "kafka",
  label: "Kafka",
  description: "A Kafka cluster's consumer groups (server text).",
  capabilities: ["metrics", "queues"],
  fields: [
    field({ key: "bootstrap_servers", label: "Bootstrap servers", type: "text", required: true, placeholder: "localhost:9092", help: "Server help.", destination: true }),
    field({ key: "sasl_mechanism", label: "SASL", type: "select", options: ["none", "plain", "scram-sha-256", "scram-sha-512"], placeholder: "none" }),
    field({ key: "username", label: "Username", type: "text", help: "For SASL." }),
    field({ key: "password", label: "Password", type: "secret", secret: true, help: "For SASL." }),
    field({ key: "tls", label: "TLS", type: "select", options: ["off", "on"], placeholder: "off" }),
    field({ key: "ca_cert", label: "CA certificate", type: "secret", secret: true, show_when: { tls: "on" }, multiline: true }),
  ],
};

const natsKind: DataSourceKindInfo = {
  kind: "nats",
  label: "NATS JetStream",
  description: "A NATS server (server text).",
  capabilities: ["metrics", "queues"],
  fields: [
    field({ key: "url", label: "Monitoring URL", type: "url", required: true, placeholder: "http://localhost:8222" }),
    field({ key: "token", label: "Bearer token", type: "secret", secret: true }),
  ],
};

function choose(label: RegExp, option: RegExp) {
  fireEvent.click(screen.getByLabelText(label));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

beforeEach(() => {
  createDataSource.mockReset();
  queryDataSource.mockReset();
  updateDataSource.mockReset();
});

describe("broker kinds in the data source form", () => {
  it("draws Kafka's fields in the reader's words, with the CA certificate as a text area once TLS is on", async () => {
    createDataSource.mockResolvedValue(dataSource({ kind: "kafka", name: "kafka-prod", config: {}, secrets_set: ["ca_cert"], capabilities: ["metrics", "queues"] }));
    render(
      <MemoryRouter>
        <I18nProvider>
          <DataSourceForm open onOpenChange={() => {}} kinds={[prometheusKind, kafkaKind, natsKind]} onSaved={() => {}} />
        </I18nProvider>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("radio", { name: /Kafka/ }));
    expect(screen.getByRole("radio", { name: /Kafka/ })).toHaveTextContent("read live over the Kafka protocol");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const bootstrap = screen.getByLabelText(/^Bootstrap servers/);
    expect(bootstrap).toHaveAttribute("placeholder", "broker-1:9092,broker-2:9092");
    expect(screen.getByText(/the port defaults to 9092/)).toBeInTheDocument();
    expect(screen.getByText(/needs Describe on the consumer groups/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^CA certificate/)).not.toBeInTheDocument();

    choose(/^SASL/, /^SCRAM-SHA-512$/);
    choose(/^TLS/, /^On$/);
    const ca = await screen.findByLabelText(/^CA certificate/);
    expect(ca.tagName).toBe("TEXTAREA");
    expect(ca).toHaveAttribute("placeholder", "-----BEGIN CERTIFICATE-----");

    fireEvent.change(bootstrap, { target: { value: "b-1:9092,b-2:9092" } });
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: "monitor" } });
    fireEvent.change(screen.getByLabelText(/^Password/), { target: { value: "pw" } });
    const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----";
    fireEvent.change(ca, { target: { value: pem } });
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));

    await waitFor(() =>
      expect(createDataSource).toHaveBeenCalledWith({
        kind: "kafka",
        config: { bootstrap_servers: "b-1:9092,b-2:9092", sasl_mechanism: "scram-sha-512", username: "monitor", tls: "on" },
        secrets: { password: "pw", ca_cert: pem },
      }),
    );
  });

  it("says moving the brokers asks for the saved credentials again, and asks for them when they move", async () => {
    const saved = dataSource({
      id: "ds-kafka",
      kind: "kafka",
      name: "kafka-prod",
      config: { bootstrap_servers: "B-1:9092,b-2:9092", sasl_mechanism: "plain", username: "monitor", tls: "off" },
      secrets_set: ["password"],
      capabilities: ["metrics", "queues"],
    });
    render(
      <MemoryRouter>
        <I18nProvider>
          <DataSourceForm open onOpenChange={() => {}} kinds={[kafkaKind]} source={saved} onSaved={() => {}} />
        </I18nProvider>
      </MemoryRouter>,
    );
    const bootstrap = screen.getByLabelText(/^Bootstrap servers/);
    expect(bootstrap).toHaveAccessibleDescription(expect.stringContaining("Changing this asks for the saved credentials again."));
    expect(screen.queryByText(/Saved credentials are never sent to a new address/)).toBeNull();

    fireEvent.change(bootstrap, { target: { value: "b-3:9092" } });
    expect(await screen.findByText(/Saved credentials are never sent to a new address/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    expect(updateDataSource).not.toHaveBeenCalled();

    fireEvent.change(bootstrap, { target: { value: "b-2:9092, b-1.:9092" } });
    await waitFor(() => expect(screen.queryByText(/Saved credentials are never sent to a new address/)).toBeNull());
    updateDataSource.mockResolvedValue(saved);
    fireEvent.click(screen.getByRole("button", { name: "Save and check" }));
    await waitFor(() => expect(updateDataSource).toHaveBeenCalled());
    expect(updateDataSource.mock.calls[0][1]).toMatchObject({ secrets: {} });
  });

  it("does not offer a RabbitMQ queue named with * as a query of its own", () => {
    expect(inventoryQuery("rabbitmq", "queue/jobs.*")).toBeNull();
    expect(inventoryQuery("rabbitmq", "queue/jobs.eu")).toBe('{"queue":"jobs.eu"}');
    expect(inventoryQuery("kafka", "group/a*b")).toBe('{"group":"a*b"}');
  });

  it("labels NATS from the locale files, falling back to the server for what they do not cover", () => {
    render(
      <MemoryRouter>
        <I18nProvider>
          <DataSourceForm open onOpenChange={() => {}} kinds={[natsKind]} onSaved={() => {}} />
        </I18nProvider>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByLabelText(/^Monitoring URL/)).toHaveAttribute("placeholder", "http://localhost:8222");
    expect(screen.getByText(/nats-server -m 8222/)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Bearer token/)).toHaveAttribute("type", "password");
  });
});

describe("Try a query on a broker source", () => {
  it("shows the kind's JSON hint and a single-point answer as values, not a chart", async () => {
    const now = Date.now();
    queryDataSource.mockResolvedValue({
      series: [
        { name: "lag", labels: { group: "billing", partition: "0", topic: "invoices" }, points: [[now, 4]] },
        { name: "lag", labels: { group: "billing", partition: "1", topic: "invoices" }, points: [[now, 30]] },
      ],
      warnings: [],
    });
    let query = "";
    const view = render(
      <MemoryRouter>
        <I18nProvider>
          <QueryPlayground sourceId="ds-kafka" kind="kafka" query={query} onQueryChange={(q) => (query = q)} />
        </I18nProvider>
      </MemoryRouter>,
    );
    const box = screen.getByLabelText("Query");
    expect(box).toHaveAttribute("placeholder", 'e.g. {"group": "billing", "metric": "lag"}');
    expect(screen.getByText(/lag_by_partition, consumers or members/)).toBeInTheDocument();

    query = '{"group": "billing", "metric": "lag_by_partition"}';
    view.rerender(
      <MemoryRouter>
        <I18nProvider>
          <QueryPlayground sourceId="ds-kafka" kind="kafka" query={query} onQueryChange={(q) => (query = q)} />
        </I18nProvider>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    const list = await screen.findByRole("list", { name: "Query result values" });
    const items = list.querySelectorAll("li");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('lag{group="billing", partition="1", topic="invoices"}30');
    expect(screen.getByText(/one point per series, whatever the range/)).toBeInTheDocument();
    expect(screen.queryByRole("img", { name: "Query result" })).not.toBeInTheDocument();
  });
});

describe("broker kind helpers", () => {
  it("turns a picked inventory entry into the kind's JSON query", () => {
    expect(inventoryQuery("kafka", "group/billing")).toBe('{"group":"billing"}');
    expect(inventoryQuery("kafka", "topic/invoices")).toBe("topic/invoices");
    expect(inventoryQuery("rabbitmq", "queue/orders/eu")).toBe('{"queue":"orders/eu"}');
    expect(inventoryQuery("nats", "stream/ORDERS")).toBe('{"stream":"ORDERS"}');
    expect(inventoryQuery("nats", "consumer/ORDERS/billing")).toBe('{"stream":"ORDERS","consumer":"billing"}');
    expect(inventoryQuery("prometheus", "http_requests_total")).toBe("http_requests_total");
    expect(inventoryQuery("aws_cloudwatch", "AWS/SQS/ApproximateNumberOfMessagesVisible")).toBe("AWS/SQS/ApproximateNumberOfMessagesVisible");
  });

  it("takes a field the schema marks multiline as multi-line, whatever its key, and a broker source as live-only", () => {
    expect(isMultilineField(kafkaKind.fields[5])).toBe(true);
    expect(isMultilineField(kafkaKind.fields[3])).toBe(false);
    expect(isMultilineField(prometheusKind.fields[0])).toBe(false);
    expect(isMultilineField(field({ key: "ca_cert", label: "CA", type: "secret", secret: true }))).toBe(false);
    expect(isMultilineField(field({ key: "notes", label: "Notes", type: "text", multiline: true }))).toBe(true);
    expect(readsLiveOnly({ capabilities: ["metrics", "queues"] })).toBe(true);
    expect(readsLiveOnly({ capabilities: ["metrics"] })).toBe(false);
    const t = (key: string) => key;
    expect(queryHint(t, "prometheus")).toBe("");
  });
});
