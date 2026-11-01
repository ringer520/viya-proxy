// Keep authentication handling local to panel requests instead of replacing
// the browser's global fetch (which also affects third-party integrations).
export async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const response = await window.fetch(input, init);
  if (response.status === 401) {
    const raw = input instanceof Request ? input.url : String(input);
    const url = new URL(raw, window.location.href);
    if (url.origin === window.location.origin && url.pathname.startsWith('/api/v1/') &&
        !/^\/api\/v1\/auth\/(login|setup|logout|status)$/.test(url.pathname)) {
      window.dispatchEvent(new Event('viya:session-expired'));
    }
  }
  return response;
}
