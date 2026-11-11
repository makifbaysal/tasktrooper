import { describe, expect, it } from "vitest";
import { isNonGithubRemote } from "./githubUrl";

describe("isNonGithubRemote", () => {
  it("flags other hosts in https, ssh and scp forms", () => {
    expect(isNonGithubRemote("https://gitlab.com/a/b.git")).toBe(true);
    expect(isNonGithubRemote("git@gitlab.com:a/b.git")).toBe(true);
    expect(isNonGithubRemote("ssh://git@bitbucket.org/a/b")).toBe(true);
  });
  it("leaves GitHub, empty and local remotes alone", () => {
    expect(isNonGithubRemote("https://github.com/a/b.git")).toBe(false);
    expect(isNonGithubRemote("git@github.com:a/b.git")).toBe(false);
    expect(isNonGithubRemote(undefined)).toBe(false);
    expect(isNonGithubRemote("/srv/git/b.git")).toBe(false);
  });
});
