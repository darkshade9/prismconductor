export interface ParsedRepo {
  owner: string;
  repo: string;
}

/**
 * Parses a GitHub repository URL (HTTPS or SSH) into owner/repo components.
 *
 * Accepted forms:
 *   https://github.com/owner/repo
 *   https://github.com/owner/repo.git
 *   git@github.com:owner/repo
 *   git@github.com:owner/repo.git
 *
 * Returns null for any other input.
 */
export function parseRepoURL(raw: string): ParsedRepo | null {
  const s = raw.trim();
  if (!s) return null;

  let path: string | null = null;

  // HTTPS form: https://github.com/owner/repo[.git]
  const httpsMatch = s.match(/^https?:\/\/github\.com\/([^/]+\/[^/]+?)(?:\.git)?\/?$/);
  if (httpsMatch) {
    path = httpsMatch[1];
  }

  // SSH form: git@github.com:owner/repo[.git]
  if (!path) {
    const sshMatch = s.match(/^git@github\.com:([^/]+\/[^/]+?)(?:\.git)?$/);
    if (sshMatch) {
      path = sshMatch[1];
    }
  }

  if (!path) return null;

  const slash = path.indexOf("/");
  if (slash <= 0 || slash === path.length - 1) return null;

  const owner = path.slice(0, slash).trim();
  const repo = path.slice(slash + 1).trim();

  if (!owner || !repo) return null;

  return { owner, repo };
}

/**
 * Derives a workspace ID slug from owner and repo.
 *
 * Uses just {repo} when no existing workspace already uses that ID;
 * otherwise falls back to {owner}-{repo}. Both are sanitized to
 * lowercase letters, numbers, and hyphens.
 */
export function deriveWorkspaceID(owner: string, repo: string, existingIDs: string[]): string {
  const sanitize = (s: string) =>
    s
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");

  const repoSlug = sanitize(repo);
  if (!existingIDs.includes(repoSlug)) return repoSlug;

  const combined = sanitize(`${owner}-${repo}`);
  return combined;
}
