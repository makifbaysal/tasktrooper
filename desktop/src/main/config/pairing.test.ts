import { describe, expect, it } from "vitest";
import { asPairingBundle, pairingSummary } from "./pairing.js";

const valid = {
  runner_token: "rtok-1",
  tm_base_url: "https://app.tasktrooper.ai",
  tenant_id: "tenant-1",
  member_uid: "member-1",
  paired_at: "2026-09-28T00:00:00Z",
  label: "Akif's MacBook",
};

/**
 * `pairing.bin` holds a bearer credential for this Mac's own tunnel session.
 * `asPairingBundle` is what stands between whatever landed on disk (or
 * crossed IPC) and the runner's stdin, so a malformed or half-written
 * document must read as "not paired" — never as a bundle with an empty
 * token, which the runner would refuse at startup with a confusing error.
 */
describe("asPairingBundle", () => {
  it("accepts a complete bundle", () => {
    expect(asPairingBundle(valid)).toEqual(valid);
  });

  it("refuses anything missing a required field", () => {
    for (const key of Object.keys(valid) as (keyof typeof valid)[]) {
      const { [key]: _drop, ...rest } = valid;
      expect(asPairingBundle(rest), `missing ${key}`).toBeNull();
    }
  });

  it("refuses an empty string for any field", () => {
    for (const key of Object.keys(valid) as (keyof typeof valid)[]) {
      expect(asPairingBundle({ ...valid, [key]: "" }), `empty ${key}`).toBeNull();
    }
  });

  it("refuses a tm_base_url that is not https and not loopback http", () => {
    for (const bad of ["http://app.tasktrooper.ai", "ftp://app.tasktrooper.ai", "not a url", ""]) {
      expect(asPairingBundle({ ...valid, tm_base_url: bad }), bad).toBeNull();
    }
  });

  it("accepts a tm_base_url that is plain http on loopback, for local trials", () => {
    for (const good of ["http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080"]) {
      expect(asPairingBundle({ ...valid, tm_base_url: good }), good).toEqual({ ...valid, tm_base_url: good });
    }
  });

  it("refuses a value with a control character — it crosses a pipe and a log line", () => {
    expect(asPairingBundle({ ...valid, label: "evil\nrunner_token: stolen" })).toBeNull();
  });

  it("refuses non-object input", () => {
    for (const raw of [null, undefined, "", 42, []]) {
      expect(asPairingBundle(raw)).toBeNull();
    }
  });
});

describe("pairingSummary", () => {
  it("drops the token and keeps everything else", () => {
    const summary = pairingSummary(valid);
    expect(summary).toEqual({
      tm_base_url: valid.tm_base_url,
      tenant_id: valid.tenant_id,
      member_uid: valid.member_uid,
      paired_at: valid.paired_at,
      label: valid.label,
    });
    expect(summary).not.toHaveProperty("runner_token");
  });
});
