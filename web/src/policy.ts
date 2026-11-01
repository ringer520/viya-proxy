// Shared types and helpers for the inheritable per-site policy (security
// headers, basic-auth identity, upstream headers, IP filters and rate limits).
// Used by both the default-policy dialog and the site form.

export type SecurityHeaders = {
  enabled: boolean;
  inherit: boolean;
  frame_options: string;
  nosniff: boolean;
  hsts: boolean;
  referrer_policy: string;
  csp: string;
};

export type SitePolicy = SecurityHeaders & {
  basic_auth_user: string;
  basic_auth_enabled: boolean;
  custom_headers: Record<string, string>;
  ip_allowlist: string[];
  ip_denylist: string[];
  rate_limit_requests: number;
  rate_limit_window: string;
  max_body_bytes: number;
  allowed_methods: string[];
  hide_server: boolean;
};

export function emptySecurityHeaders(): SecurityHeaders {
  return {
    enabled: false,
    inherit: false,
    frame_options: "",
    nosniff: false,
    hsts: false,
    referrer_policy: "",
    csp: "",
  };
}

export function emptySitePolicy(): SitePolicy {
  return {
    ...emptySecurityHeaders(),
    basic_auth_user: "",
    basic_auth_enabled: false,
    custom_headers: {},
    ip_allowlist: [],
    ip_denylist: [],
    rate_limit_requests: 0,
    rate_limit_window: "1s",
    max_body_bytes: 0,
    allowed_methods: [],
    hide_server: false,
  };
}

export const httpMethods = [
  "GET",
  "HEAD",
  "POST",
  "PUT",
  "PATCH",
  "DELETE",
  "OPTIONS",
];
