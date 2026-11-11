import { describe, expect, it } from "vitest";
import type { RemoteDevice } from "@/api";
import { connectedDevice, isSubdomain, modeLabelKey, pairableUrl, subdomainOf } from "@/lib/remoteAccess";

const device = (patch: Partial<RemoteDevice>): RemoteDevice => ({ id: "d", name: "Phone", role: "operator", created_at: "2026-11-01T00:00:00Z", ...patch });

describe("connectedDevice", () => {
  const now = Date.parse("2026-11-11T10:00:00Z");
  it("is the one phone that is neither revoked nor expired", () => {
    const list = [
      device({ id: "revoked", revoked_at: "2026-11-10T00:00:00Z" }),
      device({ id: "expired", expires_at: "2026-11-05T00:00:00Z" }),
      device({ id: "live", expires_at: "2026-12-05T00:00:00Z" }),
    ];
    expect(connectedDevice(list, now)?.id).toBe("live");
    expect(connectedDevice([device({ id: "forever", expires_at: null })], now)?.id).toBe("forever");
  });
  it("is null when nobody is connected", () => {
    expect(connectedDevice([], now)).toBeNull();
    expect(connectedDevice([device({ revoked_at: "2026-11-10T00:00:00Z" })], now)).toBeNull();
  });
});

describe("subdomains", () => {
  it("splits a hostname on the logged-in domain", () => {
    expect(subdomainOf("phone.example.com", "example.com")).toBe("phone");
    expect(subdomainOf("a.b.example.com", "example.com")).toBe("a.b");
    expect(subdomainOf("example.com", "example.com")).toBeNull();
    expect(subdomainOf("phone.other.org", "example.com")).toBeNull();
  });
  it("accepts one DNS label only", () => {
    expect(isSubdomain("tasktrooper-ab12")).toBe(true);
    expect(isSubdomain("a.b")).toBe(false);
    expect(isSubdomain("-x")).toBe(false);
    expect(isSubdomain("UPPER")).toBe(false);
  });
});

describe("labels and addresses", () => {
  it("names the own-domain choice after the provider", () => {
    expect(modeLabelKey("cloudflare", "named")).toBe("remoteAccess.tunnel.modes.named");
    expect(modeLabelKey("ngrok", "named")).toBe("remoteAccess.tunnel.modes.namedNgrok");
    expect(modeLabelKey("ngrok", "quick")).toBe("remoteAccess.tunnel.modes.quick");
  });
  it("only a connected tunnel with an https address can carry a pairing", () => {
    expect(pairableUrl({ tunnel: "connected", url: "https://a.example.com" })).toBe("https://a.example.com");
    expect(pairableUrl({ tunnel: "starting", url: "https://a.example.com" })).toBeNull();
    expect(pairableUrl({ tunnel: "connected", url: "http://a.example.com" })).toBeNull();
    expect(pairableUrl(null)).toBeNull();
  });
});
