import type { ComponentChildren } from "preact";
import { useNow } from "../hooks";
import { formatBytes, timeAgo } from "../lib";
import { Figure } from "./figure";

// Widgets for homelab infrastructure: Kopia, Syncthing, Nginx Proxy Manager
// and Komodo. Each leads with figures, then lists problems first.

/** "3h ago" split for a figure: ["3", "h ago"]. */
function ageFigure(date: Date, now: Date): [string, string] {
  const s = Math.max(0, (now.getTime() - date.getTime()) / 1000);
  if (s < 60) return ["now", ""];
  if (s < 3600) return [String(Math.floor(s / 60)), "m ago"];
  if (s < 86400) return [String(Math.floor(s / 3600)), "h ago"];
  return [String(Math.floor(s / 86400)), "d ago"];
}

function Row(props: {
  tone: string;
  name: ComponentChildren;
  meta?: ComponentChildren;
  sub?: ComponentChildren;
  title?: string;
}) {
  return (
    <li title={props.title ?? (typeof props.sub === "string" ? props.sub : undefined)}>
      <span class={`dot ${props.tone}`} />
      <span class="w-row-text">
        <span class="w-row-name">{props.name}</span>
        {props.sub && <span class={`w-row-sub ${props.tone}`}>{props.sub}</span>}
      </span>
      {props.meta && <span class="w-row-meta">{props.meta}</span>}
    </li>
  );
}

function More({ count, noun }: { count: number; noun: string }) {
  if (count <= 0) return null;
  return (
    <li class="w-more">
      + {count} more {noun}
    </li>
  );
}

// ── Kopia ────────────────────────────────────────────────────────────────

type KopiaSource = {
  path: string;
  host: string;
  status: string;
  last?: string;
  size: number;
  files: number;
  errors: number;
  state: "ok" | "running" | "stale" | "errors" | "never";
};
export type KopiaData = {
  sources: KopiaSource[];
  total_size: number;
  latest?: string;
  problems: number;
  stale_hours: number;
};

const KOPIA_TONE = { ok: "good", running: "accent", stale: "bad", errors: "warn", never: "bad" };

export function Kopia({ data }: { data: KopiaData }) {
  const now = useNow();
  if (data.sources.length === 0) return <div class="widget widget-empty">No snapshot sources</div>;
  const latest = data.latest ? new Date(data.latest) : null;
  const [age, ageUnit] = latest ? ageFigure(latest, now) : ["—", ""];
  const shown = data.sources.slice(0, 6);
  return (
    <div class="widget">
      <div class="figures">
        <Figure value={age} unit={ageUnit} label="Last backup" />
        <Figure
          value={String(data.sources.length - data.problems)}
          unit={`/${data.sources.length}`}
          label="Sources healthy"
          tone={data.problems ? "bad" : undefined}
        />
        <Figure
          value={formatBytes(data.total_size).split(" ")[0]}
          unit={formatBytes(data.total_size).split(" ")[1]}
          label="Protected"
        />
      </div>
      <ul class="w-list">
        {shown.map((s) => {
          const last = s.last ? new Date(s.last) : null;
          const sub =
            s.state === "never"
              ? "Never backed up"
              : s.state === "stale"
                ? `No snapshot since ${last ? timeAgo(last, now) : "?"}`
                : s.state === "errors"
                  ? `${s.errors} ${s.errors === 1 ? "file" : "files"} failed`
                  : s.state === "running"
                    ? "Snapshot running"
                    : null;
          return (
            <Row
              key={s.path + s.host}
              tone={KOPIA_TONE[s.state]}
              name={s.path}
              sub={sub}
              title={`${s.host} · ${s.files} files`}
              meta={last ? `${timeAgo(last, now)} · ${formatBytes(s.size)}` : "—"}
            />
          );
        })}
        <More count={data.sources.length - shown.length} noun="sources" />
      </ul>
    </div>
  );
}

// ── Syncthing ────────────────────────────────────────────────────────────

type SyncFolder = {
  id: string;
  label: string;
  state: string;
  error?: string;
  pull_errors?: number;
  need_bytes: number;
  need_items: number;
  completion: number;
};
type SyncDevice = { name: string; connected: boolean; paused?: boolean; last_seen?: string };
export type SyncthingData = {
  folders: SyncFolder[];
  devices: SyncDevice[];
  in_sync: number;
  connected: number;
  problems: number;
};

function folderStatus(f: SyncFolder): { tone: string; label: string } {
  if (f.state === "error" || f.pull_errors) return { tone: "bad", label: "Error" };
  if (f.state === "paused") return { tone: "", label: "Paused" };
  if (f.state.startsWith("sync"))
    return { tone: "accent", label: `Syncing ${Math.floor(f.completion)}%` };
  if (f.state.startsWith("scan")) return { tone: "accent", label: "Scanning" };
  if (f.need_items > 0) return { tone: "warn", label: `${f.need_items} out of sync` };
  return { tone: "good", label: "Up to date" };
}

export function Syncthing({ data }: { data: SyncthingData }) {
  const now = useNow();
  const shown = data.folders.slice(0, 6);
  return (
    <div class="widget">
      <div class="figures">
        <Figure
          value={String(data.in_sync)}
          unit={`/${data.folders.length}`}
          label="Folders up to date"
          tone={data.problems ? "bad" : undefined}
        />
        <Figure
          value={String(data.connected)}
          unit={`/${data.devices.length}`}
          label="Devices online"
        />
      </div>
      {data.folders.length > 0 && (
        <ul class="w-list">
          {shown.map((f) => {
            const st = folderStatus(f);
            const sub = f.error
              ? f.error
              : f.pull_errors
                ? `${f.pull_errors} files couldn't sync`
                : f.need_bytes > 0
                  ? `${formatBytes(f.need_bytes)} to go`
                  : null;
            return <Row key={f.id} tone={st.tone} name={f.label} sub={sub} meta={st.label} />;
          })}
          <More count={data.folders.length - shown.length} noun="folders" />
        </ul>
      )}
      {data.devices.length > 0 && (
        <div class="w-chips">
          {data.devices.map((d) => (
            <span
              class="w-chip"
              key={d.name}
              title={
                d.connected
                  ? "Connected"
                  : d.last_seen
                    ? `Last seen ${timeAgo(new Date(d.last_seen), now)}`
                    : "Never connected"
              }
            >
              <span class={`dot ${d.connected ? "good" : ""}`} />
              {d.name}
              {!d.connected && (
                <span class="w-chip-meta">
                  {d.paused
                    ? "paused"
                    : d.last_seen
                      ? timeAgo(new Date(d.last_seen), now)
                      : "never"}
                </span>
              )}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Nginx Proxy Manager ──────────────────────────────────────────────────

type ProxyHost = {
  domains: string[];
  forward_host: string;
  forward_port: number;
  enabled: boolean;
  ssl: boolean;
  error?: string;
};
type Certificate = {
  name: string;
  domains: string[];
  expires: string;
  days: number;
  hosts: number;
};
export type NPMData = {
  hosts: ProxyHost[];
  certificates: Certificate[];
  disabled: number;
  errors: number;
  expiring: number;
  warn_days: number;
};

function certTone(c: Certificate, warnDays: number): string {
  if (c.days < 3) return "bad";
  if (c.days < warnDays) return "warn";
  return "good";
}

function daysLabel(days: number): string {
  if (days < 0) return "expired";
  if (days === 0) return "today";
  return `${days} ${days === 1 ? "day" : "days"}`;
}

export function NginxProxyManager({ data }: { data: NPMData }) {
  const next = data.certificates.find((c) => c.expires && !c.expires.startsWith("0001"));
  const broken = data.hosts.filter((h) => h.error);
  const certs = data.certificates.slice(0, Math.max(0, 5 - broken.length));
  return (
    <div class="widget">
      <div class="figures">
        <Figure
          value={String(data.hosts.length - data.disabled)}
          unit={data.disabled ? `/${data.hosts.length}` : undefined}
          label="Proxy hosts"
          tone={data.errors ? "bad" : undefined}
        />
        <Figure value={String(data.certificates.length)} label="Certificates" />
        {next && (
          <Figure
            value={next.days < 0 ? "0" : String(next.days)}
            unit="d"
            label="Next expiry"
            tone={
              certTone(next, data.warn_days) === "good" ? undefined : certTone(next, data.warn_days)
            }
          />
        )}
      </div>
      <ul class="w-list">
        {broken.map((h) => (
          <Row
            key={h.domains[0]}
            tone="bad"
            name={h.domains[0]}
            sub={h.error}
            meta="Config error"
          />
        ))}
        {certs.map((c) => (
          <Row
            key={c.name}
            tone={certTone(c, data.warn_days)}
            name={c.name}
            title={c.domains.join(", ")}
            sub={c.hosts === 0 ? "Not used by any host" : null}
            meta={daysLabel(c.days)}
          />
        ))}
        <More count={data.certificates.length - certs.length} noun="certificates" />
      </ul>
    </div>
  );
}

// ── Komodo ───────────────────────────────────────────────────────────────

type KomodoStack = {
  name: string;
  state: string;
  server?: string;
  services: number;
  updates: number;
};
type KomodoUpdate = { operation: string; target?: string; success: boolean; at: string };
export type KomodoData = {
  stacks: KomodoStack[];
  running: number;
  updates: number;
  servers: { name: string; state: string }[];
  recent: KomodoUpdate[];
};

function stackTone(s: KomodoStack): string {
  switch (s.state) {
    case "running":
      return "good";
    case "unhealthy":
    case "down":
      return "bad";
    case "deploying":
    case "restarting":
    case "created":
      return "accent";
    case "stopped":
    case "paused":
      return "warn";
  }
  return "";
}

export function Komodo({ data }: { data: KomodoData }) {
  const now = useNow();
  const shown = data.stacks.slice(0, 6);
  const serversOk = data.servers.filter((s) => s.state === "Ok").length;
  return (
    <div class="widget">
      <div class="figures">
        <Figure
          value={String(data.running)}
          unit={`/${data.stacks.length}`}
          label="Stacks running"
          tone={data.stacks.some((s) => stackTone(s) === "bad") ? "bad" : undefined}
        />
        <Figure
          value={String(data.updates)}
          label={data.updates === 1 ? "Image update" : "Image updates"}
          tone={data.updates ? "accent" : undefined}
        />
        {data.servers.length > 0 && (
          <Figure
            value={String(serversOk)}
            unit={`/${data.servers.length}`}
            label="Servers reachable"
            tone={serversOk < data.servers.length ? "bad" : undefined}
          />
        )}
      </div>
      {shown.length > 0 && (
        <ul class="w-list">
          {shown.map((s) => (
            <Row
              key={s.name}
              tone={stackTone(s)}
              name={s.name}
              title={s.server ? `on ${s.server}` : undefined}
              meta={
                s.updates > 0 ? (
                  <span class="w-badge">
                    {s.updates} {s.updates === 1 ? "update" : "updates"}
                  </span>
                ) : (
                  s.state
                )
              }
            />
          ))}
          <More count={data.stacks.length - shown.length} noun="stacks" />
        </ul>
      )}
      {data.recent.length > 0 && (
        <div class="w-recent">
          <p class="eyebrow">Recent activity</p>
          <ul class="w-list">
            {data.recent.slice(0, 3).map((u, i) => (
              <Row
                key={i}
                tone={u.success ? "good" : "bad"}
                name={`${u.operation}${u.target ? ` · ${u.target}` : ""}`}
                meta={timeAgo(new Date(u.at), now)}
              />
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
