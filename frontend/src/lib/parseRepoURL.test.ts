import { describe, it, expect } from "vitest";
import { parseRepoURL, deriveWorkspaceID } from "./parseRepoURL";

describe("parseRepoURL", () => {
  it("parses HTTPS URL", () => {
    expect(parseRepoURL("https://github.com/octocat/hello-world")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });

  it("parses HTTPS URL with .git suffix", () => {
    expect(parseRepoURL("https://github.com/octocat/hello-world.git")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });

  it("parses HTTPS URL with trailing slash", () => {
    expect(parseRepoURL("https://github.com/octocat/hello-world/")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });

  it("parses SSH URL", () => {
    expect(parseRepoURL("git@github.com:octocat/hello-world")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });

  it("parses SSH URL with .git suffix", () => {
    expect(parseRepoURL("git@github.com:octocat/hello-world.git")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });

  it("handles org/repo with hyphens and dots", () => {
    expect(parseRepoURL("https://github.com/my-org/my.repo")).toEqual({
      owner: "my-org",
      repo: "my.repo",
    });
  });

  it("returns null for non-GitHub URLs", () => {
    expect(parseRepoURL("https://gitlab.com/octocat/hello-world")).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(parseRepoURL("")).toBeNull();
  });

  it("returns null for whitespace", () => {
    expect(parseRepoURL("   ")).toBeNull();
  });

  it("returns null for URL missing repo part", () => {
    expect(parseRepoURL("https://github.com/octocat")).toBeNull();
  });

  it("returns null for bare owner/repo without URL prefix", () => {
    expect(parseRepoURL("octocat/hello-world")).toBeNull();
  });

  it("trims surrounding whitespace", () => {
    expect(parseRepoURL("  https://github.com/octocat/hello-world  ")).toEqual({
      owner: "octocat",
      repo: "hello-world",
    });
  });
});

describe("deriveWorkspaceID", () => {
  it("uses repo slug when not taken", () => {
    expect(deriveWorkspaceID("octocat", "hello-world", [])).toBe("hello-world");
  });

  it("falls back to owner-repo when repo slug is taken", () => {
    expect(deriveWorkspaceID("octocat", "hello-world", ["hello-world"])).toBe("octocat-hello-world");
  });

  it("sanitizes uppercase to lowercase", () => {
    expect(deriveWorkspaceID("MyOrg", "MyRepo", [])).toBe("myrepo");
  });

  it("sanitizes special characters to hyphens", () => {
    expect(deriveWorkspaceID("my.org", "my_repo", [])).toBe("my-repo");
  });
});
