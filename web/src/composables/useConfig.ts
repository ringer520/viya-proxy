// Replays a restored preferences bundle (from ConfigTransfer) and reloads.
export function configurationRestored(
  preferences: Record<string, string>,
  message: string,
) {
  const keys = Object.keys(localStorage).filter((key) =>
    key.startsWith("viya-"),
  );
  for (const key of keys) localStorage.removeItem(key);
  for (const [key, value] of Object.entries(preferences)) {
    if (
      key.startsWith("viya-") &&
      key.length < 100 &&
      typeof value === "string" &&
      value.length < 32768
    )
      localStorage.setItem(key, value);
  }
  sessionStorage.setItem("viya-restored-message", message);
  window.location.reload();
}
