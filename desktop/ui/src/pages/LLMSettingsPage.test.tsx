import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { LLMProviderDefinition, LLMProvidersResponse, LLMProviderType } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { LLMSettingsPage } from "@/pages/LLMSettingsPage";

const listLLMProviders = vi.fn<() => Promise<LLMProvidersResponse>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listLLMProviders: () => listLLMProviders() } };
});

vi.mock("@/hooks/useSetup", () => ({
  useSetup: () => ({
    cliState: {
      flavors: [{ flavor: "claude_code", provider_type: "claude_code", label: "Claude Code" }],
      connections: [
        {
          flavor: "claude_code",
          provider_type: "claude_code",
          binary_path: "/opt/homebrew/bin/claude",
          binary_version: "2.1.0 (Claude Code)",
          catalog_path: "/Users/x/.tasktrooper/catalog",
          agent_count: 7,
          skill_count: 31,
          connected_at: "2026-10-01T00:00:00Z",
        },
      ],
    },
    refresh: vi.fn(),
  }),
}));

function provider(type: LLMProviderType, label: string, extra: Partial<LLMProviderDefinition>, configured = false) {
  return {
    definition: {
      type,
      label,
      description: `${label} long description that should not be shown`,
      default_base_url: "",
      default_model: "",
      requires_api_key: true,
      base_url_required: false,
      model_required: false,
      default_timeout_seconds: 300,
      ...extra,
    },
    config: {
      provider_type: type,
      base_url: "https://api.example.com",
      default_model: configured ? "claude-sonnet-5" : "",
      timeout_seconds: 300,
      configured,
      has_api_key: configured,
      updated_at: "",
    },
    active: false,
  };
}

describe("LLMSettingsPage", () => {
  it("lists CLIs and API connections without the machinery around them", async () => {
    listLLMProviders.mockResolvedValue({
      active_provider: "ep-1",
      providers: [
        provider("claude_code", "Claude Code", { host_executed: true, available: true }),
        provider("anthropic", "Anthropic", {}, true),
        provider("gemini", "Gemini", {}),
      ],
      endpoints: [
        {
          id: "ep-1",
          name: "Home Ollama",
          base_url: "http://127.0.0.1:11434/v1",
          default_model: "qwen3",
          timeout_seconds: 120,
          configured: true,
          has_api_key: false,
          created_at: "",
          updated_at: "",
        },
      ],
    });

    render(
      <I18nProvider>
        <LLMSettingsPage />
      </I18nProvider>,
    );

    expect(await screen.findByText("Home Ollama")).toBeInTheDocument();
    expect(screen.getByText("127.0.0.1:11434 · qwen3")).toBeInTheDocument();
    expect(screen.getByText("Default")).toBeInTheDocument();
    expect(screen.getByText("claude-sonnet-5")).toBeInTheDocument();
    expect(screen.getByText("2.1.0 (Claude Code)")).toBeInTheDocument();

    expect(screen.queryByText(/opt\/homebrew/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\.tasktrooper\/catalog/)).not.toBeInTheDocument();
    expect(screen.queryByText(/long description/)).not.toBeInTheDocument();
  });
});
