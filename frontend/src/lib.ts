import type { Config, Group, Service, ServiceStatus } from "./types";

const DASHBOARD_ICONS = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons";
const SIMPLE_ICONS = "https://cdn.jsdelivr.net/npm/simple-icons@latest/icons";

/**
 * Resolves an icon reference the way Homepage does: a URL or absolute path is
 * used as is, `si-name` is a Simple Icons logo, and anything else is a name
 * from the dashboard-icons set (`sonarr`, `sonarr.svg`, `sonarr.webp`).
 */
export function iconUrl(icon?: string): string | null {
  const value = icon?.trim();
  if (!value) return null;
  if (/^(https?:)?\/\//.test(value) || value.startsWith("/") || value.startsWith("data:")) {
    return value;
  }
  if (value.startsWith("si-")) return `${SIMPLE_ICONS}/${value.slice(3)}.svg`;
  const match = value.match(/^(.*)\.(png|svg|webp)$/i);
  const [name, ext] = match ? [match[1], match[2].toLowerCase()] : [value, "png"];
  return `${DASHBOARD_ICONS}/${ext}/${name}.${ext}`;
}

/** Two-letter fallback for services without a loadable icon. */
export function monogram(name: string): string {
  const words = name
    .replace(/[^\p{L}\p{N} ]/gu, " ")
    .trim()
    .split(/\s+/);
  if (words.length > 1) return (words[0][0] + words[1][0]).toUpperCase();
  const word = words[0] ?? "?";
  const caps = word.match(/\p{Lu}/gu);
  if (caps && caps.length >= 2) return caps.slice(0, 2).join("");
  return (word.slice(0, 2) || "?").replace(/^./, (c) => c.toUpperCase());
}

/** Scores how well a service matches a query; 0 means no match. */
export function matchScore(service: Service, query: string): number {
  const q = query.trim().toLowerCase();
  if (!q) return 0;
  const name = service.name.toLowerCase();
  if (name === q) return 100;
  if (name.startsWith(q)) return 80;
  if (name.split(/[\s\-_.]+/).some((w) => w.startsWith(q))) return 60;
  if (name.includes(q)) return 40;
  if (service.description?.toLowerCase().includes(q)) return 20;
  if (service.url?.toLowerCase().includes(q)) return 10;
  // Loose subsequence match ("qbt" → qBittorrent).
  let i = 0;
  for (const ch of name) if (ch === q[i]) i++;
  return i === q.length ? 5 : 0;
}

export function searchServices(config: Config, query: string): Service[] {
  return config.groups
    .flatMap((g) => g.services)
    .map((s) => [s, matchScore(s, query)] as const)
    .filter(([, score]) => score > 0)
    .sort((a, b) => b[1] - a[1])
    .map(([s]) => s);
}

const SEARCH_URLS = {
  google: "https://www.google.com/search?q=",
  duckduckgo: "https://duckduckgo.com/?q=",
  bing: "https://www.bing.com/search?q=",
  kagi: "https://kagi.com/search?q=",
} as const;

export function webSearchUrl(config: Config, query: string): string {
  const { provider, url } = config.header.search;
  const base = provider === "custom" ? url : SEARCH_URLS[provider];
  return base + encodeURIComponent(query);
}

/** Widget cards take two grid cells unless configured otherwise. */
export function serviceSpan(service: Service, columns: number): number {
  const span = service.widget ? (service.widget.span ?? 2) : 1;
  return Math.max(1, Math.min(span, columns));
}

/**
 * How many page columns a group takes: enough for its cards, capped at the
 * page width. Small groups end up side by side instead of each taking a row.
 */
export function groupSpan(group: Group, columns: number): number {
  if (group.columns) return Math.min(group.columns, columns);
  const cells = group.services.reduce((n, s) => n + serviceSpan(s, columns), 0);
  return Math.max(1, Math.min(cells, columns));
}

/**
 * Orders groups so small ones fill gaps beside wider ones (first fit), while
 * each row keeps the config order. Returns group indexes in display order.
 */
export function packGroups(spans: number[], columns: number): number[] {
  const rows: { free: number; items: number[] }[] = [];
  spans.forEach((span, i) => {
    const s = Math.min(span, columns);
    let row = rows.find((r) => r.free >= s);
    if (!row) {
      row = { free: columns, items: [] };
      rows.push(row);
    }
    row.free -= s;
    row.items.push(i);
  });
  return rows.flatMap((r) => r.items);
}

export function formatBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function formatDuration(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function timeAgo(date: Date, now = new Date()): string {
  const s = Math.round((now.getTime() - date.getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export function greeting(hour: number): string {
  if (hour < 5) return "Good night";
  if (hour < 12) return "Good morning";
  if (hour < 17) return "Good afternoon";
  if (hour < 22) return "Good evening";
  return "Good night";
}

let counter = 0;
/** Temporary id for items created in the editor; the server re-derives ids. */
export const newId = (prefix: string) => `${prefix}-new-${Date.now().toString(36)}-${counter++}`;

/** Container names the dashboard already points at (mirrors the server). */
export function linkedContainers(config: Config): Set<string> {
  const linked = new Set<string>();
  for (const s of config.groups.flatMap((g) => g.services)) {
    if (s.container) linked.add(s.container);
    for (const raw of [s.ping, s.url]) {
      try {
        if (raw) linked.add(new URL(raw).hostname);
      } catch {
        // not a URL
      }
    }
  }
  return linked;
}

export function statusInfo(st?: ServiceStatus): { tone: string; label: string } | null {
  const c = st?.container;
  const p = st?.ping;
  if (p?.state === "down") return { tone: "bad", label: p.error ?? `HTTP ${p.code}` };
  if (c && c.state !== "running") return { tone: "bad", label: c.state };
  if (c?.health === "unhealthy") return { tone: "warn", label: "unhealthy" };
  if (c?.health === "starting") return { tone: "warn", label: "starting" };
  if (p?.state === "up") return { tone: "good", label: `${p.latency_ms ?? 0}ms` };
  if (c) return { tone: "good", label: c.health ?? "running" };
  return null;
}

/** Counts services that have a known state (a container or a status check). */
export function countStatus(statuses: (ServiceStatus | undefined)[]): {
  up: number;
  total: number;
} {
  let up = 0;
  let total = 0;
  for (const st of statuses) {
    const info = statusInfo(st);
    if (!info) continue;
    total++;
    if (info.tone !== "bad") up++;
  }
  return { up, total };
}
