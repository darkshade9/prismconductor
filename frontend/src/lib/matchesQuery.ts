export function matchesQuery(title: string | undefined | null, q: string): boolean {
  if (!q) return true;
  if (!title) return false;
  return title.toLowerCase().includes(q.toLowerCase());
}
