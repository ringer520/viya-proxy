import { describe, expect, it } from "vitest";
import {
  deepestFirst,
  filterEntries,
  pruneSelection,
  selectionState,
  toggleAll,
  toggleEntry,
} from "../src/components/site-files/selection";
import type { Entry } from "../src/components/site-files/types";

const entry = (name: string, isDir = false): Entry => ({
  name,
  path: name,
  is_dir: isDir,
  size: 1,
  modified: "",
});

describe("site file browser selection", () => {
  const entries = [entry("b.txt"), entry("assets", true), entry("a.txt")];

  it("sorts folders first and applies the search to the visible set", () => {
    expect(filterEntries(entries, "").map((e) => e.name)).toEqual(["assets", "a.txt", "b.txt"]);
    expect(filterEntries(entries, "A").map((e) => e.name)).toEqual(["assets", "a.txt"]);
  });

  it("reports all / some / none for the header checkbox", () => {
    const visible = filterEntries(entries, "");
    expect(selectionState(new Set(), visible)).toEqual({ count: 0, all: false, some: false });
    expect(selectionState(new Set(["assets"]), visible)).toEqual({ count: 1, all: false, some: true });
    expect(selectionState(new Set(["assets", "a.txt", "b.txt"]), visible)).toEqual({ count: 3, all: true, some: false });
    expect(selectionState(new Set(["assets"]), [])).toEqual({ count: 0, all: false, some: false });
  });

  it("select all only touches visible rows and never hidden search results", () => {
    const visible = filterEntries(entries, "a");
    const next = toggleAll(new Set<string>(), visible, true);
    expect([...next].sort()).toEqual(["a.txt", "assets"]);
    expect(toggleAll(next, visible, false).size).toBe(0);
    expect(toggleEntry(new Set(["a.txt"]), "b.txt", true).size).toBe(2);
    expect(toggleEntry(new Set(["a.txt"]), "a.txt", false).size).toBe(0);
  });

  it("prunes selections after a refresh or directory change", () => {
    const kept = pruneSelection(new Set(["a.txt", "gone.txt"]), filterEntries(entries, "a"));
    expect([...kept]).toEqual(["a.txt"]);
  });

  it("deletes children before their parent folder", () => {
    const nested = [entry("assets", true), entry("assets/deep", true), entry("assets/deep/x.txt")];
    expect(deepestFirst(nested).map((e) => e.path)).toEqual([
      "assets/deep/x.txt",
      "assets/deep",
      "assets",
    ]);
  });
});
