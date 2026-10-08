import type { MCPAccess, MCPEnvSchemaField, MCPServerCreateInput, MCPServerUpdateInput, MCPServerView } from "@/api";
import { MASKED_SECRET_VALUE } from "@/api";
import type { MCPTemplate } from "@/lib/mcpTemplates";

export const CUSTOM_TEMPLATE_ID = "__custom__";

/** A server someone adds reaches no agent until an agent names it. */
export const NEW_SERVER_ACCESS: MCPAccess = "listed";

export interface MCPServerFormState {
  id: string;
  enabled: boolean;
  transport: "stdio" | "http";
  command: string;
  argsText: string;
  url: string;
  env: Record<string, string>;
  headers: Record<string, string>;
  allowedToolsText: string;
  access: MCPAccess;
  secretDrafts: Record<string, string>;
  storedSecrets: Set<string>;
}

export function isMaskedSecret(value: string | undefined): boolean {
  if (!value) return false;
  return value === MASKED_SECRET_VALUE;
}

export function isEnvPlaceholder(value: string): boolean {
  return /^\$\{[A-Z0-9_]+\}$/.test(value);
}

export function resolveSecretFields(
  server: Pick<MCPServerView, "env" | "headers" | "secret_fields" | "env_schema">,
): Set<string> {
  const fields = new Set(server.secret_fields ?? []);
  for (const field of server.env_schema ?? []) {
    if (field.secret) {
      fields.add(field.key);
    }
  }
  for (const [key, value] of Object.entries(server.env ?? {})) {
    if (isEnvPlaceholder(value) || /TOKEN|SECRET|API_KEY|PASSWORD/i.test(key)) {
      fields.add(key);
    }
  }
  for (const [key, value] of Object.entries(server.headers ?? {})) {
    if (isEnvPlaceholder(value) || /TOKEN|SECRET|API_KEY|PASSWORD|AUTHORIZATION/i.test(key)) {
      fields.add(`header:${key}`);
    }
  }
  return fields;
}

export function emptyFormState(): MCPServerFormState {
  return {
    id: "",
    enabled: false,
    transport: "stdio",
    command: "",
    argsText: "",
    url: "",
    env: {},
    headers: {},
    allowedToolsText: "",
    access: NEW_SERVER_ACCESS,
    secretDrafts: {},
    storedSecrets: new Set(),
  };
}

export function formStateFromTemplate(template: MCPTemplate): MCPServerFormState {
  const secretFields = resolveSecretFields({
    env: template.env,
    headers: template.headers,
    secret_fields: template.secret_fields,
    env_schema: template.env_schema,
  });
  const env = { ...(template.env ?? {}) };
  const headers = { ...(template.headers ?? {}) };
  const storedSecrets = new Set<string>();

  for (const key of secretFields) {
    if (key.startsWith("header:")) continue;
    const value = env[key];
    if (value && isEnvPlaceholder(value)) {
      env[key] = "";
    }
  }
  for (const key of secretFields) {
    if (!key.startsWith("header:")) continue;
    const headerKey = key.slice("header:".length);
    const value = headers[headerKey];
    if (value && isEnvPlaceholder(value)) {
      headers[headerKey] = "";
      storedSecrets.add(key);
    }
  }

  return {
    id: template.id,
    enabled: template.enabled,
    transport: template.transport === "http" ? "http" : "stdio",
    command: template.command ?? "",
    argsText: formatLines(template.args ?? []),
    url: template.url ?? "",
    env,
    headers,
    allowedToolsText: formatLines(template.allowed_tools ?? []),
    access: template.access ?? NEW_SERVER_ACCESS,
    secretDrafts: {},
    storedSecrets,
  };
}

export function formStateFromServer(server: MCPServerView): MCPServerFormState {
  const secretFields = resolveSecretFields(server);
  const env = { ...(server.env ?? {}) };
  const headers = { ...(server.headers ?? {}) };
  const storedSecrets = new Set<string>();

  for (const key of secretFields) {
    if (key.startsWith("header:")) {
      const headerKey = key.slice("header:".length);
      const value = headers[headerKey];
      if (isMaskedSecret(value) || isEnvPlaceholder(value)) {
        headers[headerKey] = "";
        storedSecrets.add(key);
      }
      continue;
    }
    const value = env[key];
    if (isMaskedSecret(value) || isEnvPlaceholder(value)) {
      env[key] = "";
      storedSecrets.add(key);
    }
  }

  return {
    id: server.id,
    enabled: server.enabled,
    transport: server.transport === "http" ? "http" : "stdio",
    command: server.command ?? "",
    argsText: formatLines(server.args ?? []),
    url: server.url ?? "",
    env,
    headers,
    allowedToolsText: formatLines(server.allowed_tools ?? []),
    access: server.access ?? "all",
    secretDrafts: {},
    storedSecrets,
  };
}

export function formStateFromCustom(): MCPServerFormState {
  return {
    ...emptyFormState(),
    transport: "stdio",
  };
}

export function parseLines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

export function formatLines(values: string[]): string {
  return values.join("\n");
}

function buildEnvPayload(
  env: Record<string, string>,
  secretFields: Set<string>,
  secretDrafts: Record<string, string>,
  isUpdate: boolean,
): Record<string, string> | undefined {
  const result: Record<string, string> = {};
  let hasValue = false;

  for (const [key, value] of Object.entries(env)) {
    if (secretFields.has(key)) {
      const draft = secretDrafts[key]?.trim();
      if (draft) {
        result[key] = draft;
        hasValue = true;
      } else if (!isUpdate && value.trim()) {
        result[key] = value.trim();
        hasValue = true;
      }
      continue;
    }
    if (value.trim()) {
      result[key] = value.trim();
      hasValue = true;
    }
  }

  for (const [key, draft] of Object.entries(secretDrafts)) {
    if (key.startsWith("header:")) continue;
    if (draft.trim() && !(key in result)) {
      result[key] = draft.trim();
      hasValue = true;
    }
  }

  return hasValue ? result : undefined;
}

function buildHeadersPayload(
  headers: Record<string, string>,
  secretFields: Set<string>,
  secretDrafts: Record<string, string>,
  isUpdate: boolean,
): Record<string, string> | undefined {
  const result: Record<string, string> = {};
  let hasValue = false;

  for (const [key, value] of Object.entries(headers)) {
    const secretKey = `header:${key}`;
    if (secretFields.has(secretKey)) {
      const draft = secretDrafts[secretKey]?.trim();
      if (draft) {
        result[key] = draft;
        hasValue = true;
      } else if (!isUpdate && value.trim()) {
        result[key] = value.trim();
        hasValue = true;
      }
      continue;
    }
    if (value.trim()) {
      result[key] = value.trim();
      hasValue = true;
    }
  }

  for (const [key, draft] of Object.entries(secretDrafts)) {
    if (!key.startsWith("header:")) continue;
    const headerKey = key.slice("header:".length);
    if (draft.trim() && !(headerKey in result)) {
      result[headerKey] = draft.trim();
      hasValue = true;
    }
  }

  return hasValue ? result : undefined;
}

function buildSecretsPayload(secretDrafts: Record<string, string>): Record<string, string> | undefined {
  const secrets: Record<string, string> = {};
  for (const [key, value] of Object.entries(secretDrafts)) {
    if (value.trim()) {
      secrets[key] = value.trim();
    }
  }
  return Object.keys(secrets).length > 0 ? secrets : undefined;
}

export function buildCreatePayload(form: MCPServerFormState): MCPServerCreateInput {
  const secretFields = resolveSecretFields({
    env: form.env,
    headers: form.headers,
    secret_fields: [],
    env_schema: [],
  });
  for (const key of form.storedSecrets) {
    secretFields.add(key);
  }
  for (const key of Object.keys(form.secretDrafts)) {
    secretFields.add(key);
  }

  const payload: MCPServerCreateInput = {
    id: form.id.trim(),
    enabled: form.enabled,
    transport: form.transport,
    command: form.command.trim() || undefined,
    args: parseLines(form.argsText),
    url: form.url.trim() || undefined,
    allowed_tools: parseLines(form.allowedToolsText),
    access: form.access,
  };

  const env = buildEnvPayload(form.env, secretFields, form.secretDrafts, false);
  const headers = buildHeadersPayload(form.headers, secretFields, form.secretDrafts, false);
  if (env) payload.env = env;
  if (headers) payload.headers = headers;

  return payload;
}

export function buildUpdatePayload(form: MCPServerFormState, server: MCPServerView): MCPServerUpdateInput {
  const secretFields = resolveSecretFields(server);
  for (const key of form.storedSecrets) {
    secretFields.add(key);
  }

  const payload: MCPServerUpdateInput = {
    enabled: form.enabled,
    transport: form.transport,
    command: form.command.trim(),
    args: parseLines(form.argsText),
    url: form.url.trim(),
    allowed_tools: parseLines(form.allowedToolsText),
    access: form.access,
  };

  const env = buildEnvPayload(form.env, secretFields, form.secretDrafts, true);
  const headers = buildHeadersPayload(form.headers, secretFields, form.secretDrafts, true);
  const secrets = buildSecretsPayload(form.secretDrafts);
  if (env) payload.env = env;
  if (headers) payload.headers = headers;
  if (secrets) payload.secrets = secrets;

  return payload;
}

export function schemaFieldsFromTemplate(template: MCPTemplate): MCPEnvSchemaField[] {
  if (template.env_schema?.length) {
    return template.env_schema;
  }
  const fields: MCPEnvSchemaField[] = [];
  const secretFields = resolveSecretFields({
    env: template.env,
    headers: template.headers,
    secret_fields: template.secret_fields,
    env_schema: template.env_schema,
  });
  for (const key of secretFields) {
    if (key.startsWith("header:")) continue;
    fields.push({ key, secret: true });
  }
  return fields;
}
