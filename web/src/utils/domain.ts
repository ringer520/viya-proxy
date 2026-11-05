// Pure helpers shared by the site form, the list and the grouping logic.
// No reactive state lives here on purpose.
import { t } from "../i18n";

export const siteIconColors = [
  { key: "blue", label: t("海蓝"), value: "#3b82f6" },
  { key: "teal", label: t("青绿"), value: "#14b8a6" },
  { key: "green", label: t("草绿"), value: "#22c55e" },
  { key: "lime", label: t("浅绿"), value: "#84cc16" },
  { key: "amber", label: t("琥珀"), value: "#f59e0b" },
  { key: "orange", label: t("橙色"), value: "#f97316" },
  { key: "rose", label: t("玫红"), value: "#f43f5e" },
  { key: "pink", label: t("粉色"), value: "#ec4899" },
  { key: "violet", label: t("紫色"), value: "#8b5cf6" },
  { key: "slate", label: t("石墨"), value: "#64748b" },
] as const;
export function siteIconStyle(key: string) {
  const color =
    siteIconColors.find((item) => item.key === key) || siteIconColors[0];
  return {
    color: color.value,
    backgroundColor: `${color.value}18`,
    borderColor: `${color.value}35`,
  };
}
export function canonicalDomain(domain: string) {
  return domain.toLowerCase().replace(/^\*\./, "");
}
export function isWithinDomain(domain: string, parent: string) {
  const child = canonicalDomain(domain),
    base = canonicalDomain(parent);
  return child === base || child.endsWith(`.${base}`);
}

// Two-label public suffixes: for these the registrable domain is three labels
// (e.g. "app.example.co.uk" -> "example.co.uk"), otherwise it's the classic
// last-two (e.g. "app.example.com" -> "example.com"). Kept deliberately small —
// a full Public Suffix List is overkill for a self-hosted panel. Add a label
// here when a real domain mis-groups.
const multiLabelTLDs = new Set([
  "com.cn", "net.cn", "org.cn", "gov.cn", "edu.cn", "ac.cn",
  "co.uk", "org.uk", "me.uk", "ltd.uk", "net.uk", "ac.uk", "gov.uk", "nhs.uk",
  "co.jp", "ne.jp", "or.jp", "ac.jp", "go.jp",
  "com.au", "net.au", "org.au", "edu.au", "gov.au", "id.au",
  "com.br", "net.br", "org.br", "gov.br",
  "com.hk", "net.hk", "org.hk", "gov.hk", "edu.hk",
  "com.tw", "net.tw", "org.tw", "edu.tw", "gov.tw", "idv.tw",
  "com.sg", "net.sg", "org.sg", "edu.sg", "gov.sg",
  "co.in", "net.in", "org.in", "gov.in", "ac.in",
  "co.nz", "net.nz", "org.nz", "govt.nz",
  "co.kr", "or.kr", "ne.kr", "go.kr",
  "com.ru", "net.ru", "org.ru",
  "com.mx", "com.ar", "com.co", "com.ua", "com.tr", "com.my", "com.vn", "com.pl",
]);

// Registrable domain: the level at which sites should group. "app.example.com"
// and "old.example.com" both fold under "example.com" even when no explicit
// "example.com" site exists yet.
export function registrableDomain(domain: string) {
  const labels = canonicalDomain(domain).split(".").filter(Boolean);
  if (labels.length <= 2) return labels.join(".");
  const lastTwo = labels.slice(-2).join(".");
  return multiLabelTLDs.has(lastTwo)
    ? labels.slice(-3).join(".")
    : lastTwo;
}
