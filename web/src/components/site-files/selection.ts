import type { Entry } from "./types";

/** Pure selection helpers so the checkbox rules can be unit-tested in node. */
export function filterEntries(entries: Entry[], search: string): Entry[] {
  const needle = search.trim().toLowerCase();
  return entries
    .filter((entry) => entry.name.toLowerCase().includes(needle))
    .sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name));
}

export function selectionState(selected: ReadonlySet<string>, visible: Entry[]) {
  const count = visible.reduce((n, entry) => n + (selected.has(entry.path) ? 1 : 0), 0);
  return { count, all: visible.length > 0 && count === visible.length, some: count > 0 && count < visible.length };
}

export function toggleEntry(selected: ReadonlySet<string>, path: string, checked: boolean): Set<string> {
  const next = new Set(selected);
  if (checked) next.add(path);
  else next.delete(path);
  return next;
}

/** "Select all" applies to what the user can currently see, never hidden matches. */
export function toggleAll(selected: ReadonlySet<string>, visible: Entry[], checked: boolean): Set<string> {
  const next = new Set(selected);
  visible.forEach((entry) => (checked ? next.add(entry.path) : next.delete(entry.path)));
  return next;
}

/** Drop selections that disappeared after a refresh, directory change or search. */
export function pruneSelection(selected: ReadonlySet<string>, visible: Entry[]): Set<string> {
  const allowed = new Set(visible.map((entry) => entry.path));
  return new Set([...selected].filter((path) => allowed.has(path)));
}

/** Delete children before parents so a recursive directory delete cannot turn a
 * later child request into a spurious 404. */
export function deepestFirst(entries: Entry[]): Entry[] {
  return [...entries].sort((a, b) => b.path.split("/").length - a.path.split("/").length);
}
