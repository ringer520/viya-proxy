// Single source of truth for the static-site file endpoints and the CSRF
// header, so the drawer, the upload composable and the preview pane cannot
// drift apart. Every caller passes only the query/tail of the URL.
export function fileApi(hostId: number, query = ""): string {
  return `/api/v1/proxy-hosts/${hostId}/files${query}`;
}

export function fileHeaders(csrf: string, json = false): Record<string, string> {
  const h: Record<string, string> = { "X-CSRF-Token": csrf };
  if (json) h["Content-Type"] = "application/json";
  return h;
}
