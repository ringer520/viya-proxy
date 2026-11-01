// Shared type definitions for the Viya console. Kept dependency-light so the
// domain composables and page/modal components can all import from one place.
import type { SecurityHeaders } from "./policy";

export type CertificateSource = {
  kind: string;
  domain?: string;
  label: string;
  status: string;
  not_after?: string;
};

export type AutomaticCertificate = {
  domain: string;
  source: CertificateSource;
  sites: string[];
};

export type Host = {
  id: number;
  listener_id: string;
  certificate_source?: CertificateSource;
  name: string;
  favorite?:boolean;
  tags?:string[];
  domain: string;
  upstream: string;
  enabled: boolean;
  version: number;
  tls_mode: string;
  path: string;
  icon_color: string;
  dns_provider_id?: number;
  custom_certificate_id?: number;
  custom_headers: Record<string, string>;
  basic_auth_enabled: boolean;
  basic_auth_user?: string;
  ip_allowlist: string[];
  ip_denylist: string[];
  rate_limit_requests: number;
  rate_limit_window?: string;
  type: string;
  root_path?: string;
  spa_fallback?: boolean;
  browse?: boolean;
  redirect_target?: string;
  redirect_status?: number;
  redirect_keep_path?: boolean;
  security_headers?: SecurityHeaders;
  max_body_bytes?: number;
  allowed_methods?: string[];
  hide_server?: boolean;
};

export type SiteGroup = { domain: string; hosts: Host[] };

export type Revision = {
  id: number;
  status: string;
  error?: string;
  created_at: string;
};

export type Provider = {
  id: number;
  name: string;
  provider_type: string;
  has_credentials: boolean;
};

export type DNSZoneOption = { name: string; status?: string };

export type Layer4Rule = {
  id: number;
  name: string;
  listen_address: string;
  listen_port: number;
  upstream_address: string;
  protocol: string;
  enabled: boolean;
  version: number;
};

export type DDNSRecord = {
  id: number;
  name: string;
  comment: string;
  dns_provider_id: number;
  zone: string;
  record_name: string;
  record_type: "A" | "AAAA" | "CNAME";
  cname_target?: string;
  ip_source: "interface" | "http" | "";
  interface_name: string;
  interface_ip?: string;
  ttl_seconds: number;
  interval_minutes: number;
  enabled: boolean;
  last_ip: string;
  last_run_at: number;
  last_error: string;
};

export type NetworkInterfaceAddress = {
  ip: string;
  family: "ipv4" | "ipv6";
  scope: string;
  temporary?: boolean;
};

export type NetworkInterface = {
  name: string;
  index: number;
  addresses: NetworkInterfaceAddress[];
};

export type Certificate = {
  id: number;
  name: string;
  dns_names: string[];
  not_after: string;
};

export type ManagedCertificate = {
  id: number;
  name: string;
  domain: string;
  method: string;
  status: string;
  progress: number;
  progress_message: string;
  progress_attempt: number;
  last_error?: string;
  not_after?: string;
  issuer?: string;
  email?: string;
  dns_provider_id?: number;
};

export type IPBan = {
  id: number;
  domain: string;
  ip: string;
  reason: string;
  expires_at: number;
  source?: string;
};

export type ExpiringCertificate = {
  domain: string;
  kind: string;
  label: string;
  not_after: string;
  days_left: number;
  expired: boolean;
  sites: string[];
};

export type UpstreamHealth = {
  ok: boolean;
  latency_ms: number;
  checked_at: number;
  target: string;
  error?: string;
  failures: number;
};

export type Palette = "light" | "dark" | "aurora" | "ember" | "cyber";
export type ThemeChoice = "system" | Palette;

export type WebListener = {
  id: string;
  protocol: "http" | "https";
  port: number;
  copy_from?: string;
};

export type Counters = {
  requests: number;
  errors: number;
  upload: number;
  download: number;
};

export type SiteTraffic = {
  total: Counters;
  recent: Counters;
  hourly: ({ hour: number } & Counters)[];
  daily?: ({ day: number } & Counters)[];
};

export type RuleLogState = {
  events: Record<string, any>[];
  loading: boolean;
  error: string;
  loadedAt: number;
};

export type TrafficRankRow = {
  key: string;
  kind: "site" | "forward";
  label: string;
  bytes: number;
  requests: number;
};
