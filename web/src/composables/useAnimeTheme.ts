// The three collector palettes pull in wallpaper rules and keyframes that the
// default system theme never uses. Load that stylesheet only when a collector
// palette is actually selected; theme-init.js has already set data-palette
// before first paint, so there is no light-theme flash while it arrives.
const animePalettes = new Set(["sakura", "neon", "lunar"]);
let loading: Promise<unknown> | null = null;

export function ensureAnimeTheme(palette: string): void {
  if (!animePalettes.has(palette) || loading) return;
  loading = import("../anime-themes.css");
}
