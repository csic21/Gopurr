import type { Page } from "./types";

function sameJSON(a: any, b: any): boolean {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (!sameJSON(a[i], b[i])) return false;
    return true;
  }
  const ak = Object.keys(a), bk = Object.keys(b);
  if (ak.length !== bk.length) return false;
  for (const key of ak) if (!(key in b) || !sameJSON(a[key], b[key])) return false;
  return true;
}

/**
 * Snapshots cross a JSON boundary and contain fresh objects even when their
 * values did not change. Reuse the previous identities so NSH keeps rows,
 * lists and controls still, and refresh can skip a commit when the visible
 * page did not change.
 */
export function mergeDesktopSnapshot(previous: any, next: any, requestedPage?: Page): any {
  if (!previous) return next;
  if (!next || typeof next !== "object") return next;
  // The backend writes live traffic and connection counts into every
  // snapshot, and sampling history changes every second as well. Keep them
  // out of static pages so a sample alone does not re-render controls that
  // are not visible. Navigation fetches the requested page after switching.
  if (requestedPage && requestedPage !== "overview" && next.status && previous.status) {
    next.status.traffic = previous.status.traffic;
    if (requestedPage !== "conns") next.status.connections = previous.status.connections;
  }
  if (requestedPage && requestedPage !== "overview" && "history" in next) {
    next.history = previous.history;
  }
  if (sameJSON(previous, next)) return previous;
  if (
    Array.isArray(previous) ||
    Array.isArray(next) ||
    typeof previous !== "object" ||
    typeof next !== "object"
  ) {
    return next;
  }
  const result: any = {};
  // Page snapshots omit fields that belong to other pages. Retaining an
  // absent key as `undefined` makes the next comparison see a different key
  // count and defeats both identity reuse and the no-commit fast path.
  for (const key of Object.keys(next)) {
    result[key] = mergeDesktopSnapshot(previous[key], next[key]);
  }
  return result;
}
