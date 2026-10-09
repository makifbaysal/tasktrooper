import { describe, expect, it } from "vitest";
import { selectedMCPServers, withAllowedMCPServers } from "@/lib/toolPolicy";

describe("selectedMCPServers", () => {
  it("keeps ids no server list knows about", () => {
    expect(selectedMCPServers({ allow_mcp_servers: ["gitlab", "notes"] })).toEqual(["gitlab", "notes"]);
  });

  it("is empty for a policy without a list", () => {
    expect(selectedMCPServers({})).toEqual([]);
  });

  it("hides TaskTrooper's own server, which is display-only", () => {
    expect(selectedMCPServers({ allow_mcp_servers: ["TaskTrooper", "gitlab"] })).toEqual(["gitlab"]);
  });
});

describe("withAllowedMCPServers", () => {
  it("never writes tasktrooper into the list", () => {
    expect(withAllowedMCPServers({}, ["tasktrooper", "gitlab"]).allow_mcp_servers).toEqual(["gitlab"]);
  });

  it("clears the list when only tasktrooper is left, so it stays 'every server'", () => {
    expect(withAllowedMCPServers({ allow_mcp_servers: ["gitlab"] }, ["tasktrooper"])).not.toHaveProperty("allow_mcp_servers");
  });
});
