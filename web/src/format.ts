// Shared formatting helpers. Keeping them outside App.vue lets the overview
// widgets reuse the exact same units as the tables.
export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let i = -1;
  do {
    value /= 1024;
    i++;
  } while (value >= 1024 && i < 3);
  return `${value.toFixed(1)} ${units[i]}`;
}
