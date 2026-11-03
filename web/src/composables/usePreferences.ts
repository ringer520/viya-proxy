// Browser preferences (theme, refresh cadence, layout toggles) live here as a
// module singleton so page components can read them without prop-drilling.
import { ref } from "vue";
import type { Palette, ThemeChoice } from "../types";

export const paletteChoices: Palette[] = [
  "light",
  "dark",
  "aurora",
  "ember",
  "cyber",
  "sakura",
  "neon",
  "lunar",
];
const savedTheme = localStorage.getItem("viya-theme");
export const themeChoice = ref<ThemeChoice>(
  savedTheme === "system" || paletteChoices.includes(savedTheme as Palette)
    ? (savedTheme as ThemeChoice)
    : "system",
);
export function storedNumber(
  key: string,
  allowed: number[],
  fallback: number,
) {
  const value = Number(localStorage.getItem(key));
  return allowed.includes(value) ? value : fallback;
}
export const refreshInterval = ref(
  storedNumber("viya-refresh-seconds", [5, 15, 30, 60], 5),
);
export const autoRefresh = ref(localStorage.getItem("viya-auto-refresh") !== "false");
export const pauseHidden = ref(localStorage.getItem("viya-pause-hidden") !== "false");
export const trafficCharts = ref(localStorage.getItem("viya-traffic-charts") !== "false");
export const previewLimit = ref(storedNumber("viya-log-preview-limit", [3, 5, 10], 5));
export const landingPage = ref(localStorage.getItem("viya-landing-page") || "overview");
export const documentHidden = ref(document.hidden);
export const themeMedia = window.matchMedia("(prefers-color-scheme: dark)");
export const panelAccessURL = window.location.origin;
export const compactMode = ref(localStorage.getItem("viya-compact-mode") === "true");
export const simpleDisplay = ref(localStorage.getItem("viya-simple-display") === "true");
export const panelAccessPort =
  window.location.port ||
  (window.location.protocol === "https:" ? "443" : "80");

export function resolvePalette(): Palette {
  if (themeChoice.value === "system")
    return themeMedia.matches ? "dark" : "light";
  return themeChoice.value;
}
export function applyPanelSettings() {
  const palette = resolvePalette();
  const root = document.documentElement;
  // data-theme keeps the legacy light/dark rules alive; data-palette drives the
  // new token layer, so every palette is a real theme instead of a tint.
  root.dataset.theme = palette === "light" || palette === "sakura" ? "light" : "dark";
  root.dataset.palette = palette;
  root.dataset.density = compactMode.value ? "compact" : "comfortable";
  root.dataset.copy = simpleDisplay.value ? "compact" : "full";
  const values: Record<string, string> = {
    "viya-theme": themeChoice.value,
    "viya-refresh-seconds": String(refreshInterval.value),
    "viya-compact-mode": String(compactMode.value),
    "viya-simple-display": String(simpleDisplay.value),
    "viya-auto-refresh": String(autoRefresh.value),
    "viya-pause-hidden": String(pauseHidden.value),
    "viya-traffic-charts": String(trafficCharts.value),
    "viya-log-preview-limit": String(previewLimit.value),
    "viya-landing-page": landingPage.value,
  };
  for (const [key, value] of Object.entries(values))
    localStorage.setItem(key, value);
}
