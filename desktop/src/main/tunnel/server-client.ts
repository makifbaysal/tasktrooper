/**
 * The server's management API for the remote listener (`/v1/remote-access/*`,
 * contract section 9). Loopback only, with the same bearer the page uses.
 */

export interface ServerEndpoint {
  base: string;
  token: string;
}

export interface RemoteListenerClient {
  enable(server: ServerEndpoint, request: { preferredPort: number; serverName: string }): Promise<number>;
  disable(server: ServerEndpoint): Promise<void>;
}

type FetchLike = (url: string, init: { method: string; headers: Record<string, string>; body?: string; signal?: AbortSignal }) => Promise<{
  ok: boolean;
  status: number;
  json(): Promise<unknown>;
}>;

export function createListenerClient(fetchFn: FetchLike = fetch as unknown as FetchLike): RemoteListenerClient {
  const call = async (server: ServerEndpoint, method: "POST" | "PUT", route: string, body?: unknown) => {
    const response = await fetchFn(`${server.base}${route}`, {
      method,
      headers: { Authorization: `Bearer ${server.token}`, "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) throw new Error(`the local server answered ${response.status} to ${route}`);
    return response.json();
  };
  return {
    enable: async (server, { preferredPort, serverName }) => {
      if (serverName !== "") await call(server, "PUT", "/v1/remote-access/settings", { server_name: serverName }).catch(() => undefined);
      const status = (await call(server, "POST", "/v1/remote-access/enable", { preferred_port: preferredPort })) as { port?: unknown };
      if (typeof status.port !== "number" || !Number.isInteger(status.port) || status.port <= 0) {
        throw new Error("the local server did not report a remote access port");
      }
      return status.port;
    },
    disable: async (server) => {
      await call(server, "POST", "/v1/remote-access/disable");
    },
  };
}
