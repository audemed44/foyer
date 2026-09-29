import { ArrowDown, ArrowUp, LoaderCircle, Timer } from "lucide-preact";
import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api";
import { usePoll, useNow } from "../hooks";
import { timeAgo } from "../lib";
import { Dialog } from "../components/ui";
import type { ActionResult, Service } from "../types";
import { Figure } from "./figure";
import {
  Komodo,
  Kopia,
  NginxProxyManager,
  Syncthing,
  type KomodoData,
  type KopiaData,
  type NPMData,
  type SyncthingData,
} from "./infra";

export function WidgetBody({ service }: { service: Service }) {
  const { data, error, refresh } = usePoll<unknown>(() => api.widget(service.id), 60000, [
    service.id,
  ]);
  const type = service.widget?.type;

  if (error && !data) return <div class="widget widget-error">{error}</div>;
  if (!data) return <div class="widget widget-loading skeleton" />;
  switch (type) {
    case "uptimekuma":
      return <UptimeKuma data={data as KumaData} />;
    case "speedtest":
      return <Speedtest data={data as SpeedData} />;
    case "calendar":
      return <Calendar data={data as CalendarData} />;
    case "app":
      return <AppWidget data={data as AppData} service={service} onChange={refresh} />;
    case "kopia":
      return <Kopia data={data as KopiaData} />;
    case "syncthing":
      return <Syncthing data={data as SyncthingData} />;
    case "npm":
      return <NginxProxyManager data={data as NPMData} />;
    case "komodo":
      return <Komodo data={data as KomodoData} />;
  }
  return <div class="widget widget-error">Unknown widget “{type}”</div>;
}

type KumaMonitor = {
  name: string;
  status: string;
  uptime: number | null;
  ping: number | null;
  history: string[];
};
type KumaData = {
  up: number;
  down: number;
  total: number;
  uptime?: number;
  incident?: string;
  monitors: KumaMonitor[];
};

function UptimeKuma({ data }: { data: KumaData }) {
  const uptime = data.uptime;
  return (
    <div class="widget kuma">
      <div class="figures">
        {uptime !== undefined && (
          <Figure
            value={uptime >= 99.95 ? "100" : uptime.toFixed(2)}
            unit="%"
            label="Uptime · 24h"
          />
        )}
        <Figure
          value={String(data.up)}
          unit={`/${data.total}`}
          label="Monitors up"
          tone={data.down ? "bad" : undefined}
        />
      </div>
      {data.incident && <div class="kuma-incident">{data.incident}</div>}
      <ul class="kuma-list">
        {data.monitors.slice(0, 8).map((m) => (
          <li key={m.name}>
            <span
              class={`dot ${m.status === "up" ? "good" : m.status === "down" ? "bad" : "warn"}`}
            />
            <span class="kuma-name">{m.name}</span>
            <span class="beats" aria-hidden="true">
              {m.history.slice(-24).map((b, i) => (
                <i key={i} class={b} />
              ))}
            </span>
            <span class="kuma-ping">{m.ping != null ? `${m.ping} ms` : ""}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

type SpeedData = { download_mbps: number; upload_mbps: number; ping_ms: number; at?: string };

function Speedtest({ data }: { data: SpeedData }) {
  const now = useNow();
  const at = data.at ? new Date(data.at.replace(" ", "T")) : null;
  const fmt = (v: number) => (v >= 100 ? v.toFixed(0) : v.toFixed(1));
  return (
    <div class="widget speed">
      <div class="figures">
        <Figure
          value={fmt(data.download_mbps)}
          unit="Mbps"
          label="Download"
          icon={<ArrowDown size={11} />}
        />
        <Figure
          value={fmt(data.upload_mbps)}
          unit="Mbps"
          label="Upload"
          icon={<ArrowUp size={11} />}
        />
        <Figure value={data.ping_ms.toFixed(0)} unit="ms" label="Ping" icon={<Timer size={11} />} />
      </div>
      {at && !isNaN(at.getTime()) && (
        <p class="eyebrow widget-foot">Last test {timeAgo(at, now)}</p>
      )}
    </div>
  );
}

type CalendarEvent = { title: string; start: string; all_day: boolean };
type CalendarData = { events: CalendarEvent[] };

function Calendar({ data }: { data: CalendarData }) {
  const now = useNow();
  if (data.events.length === 0) return <div class="widget widget-empty">Nothing coming up</div>;
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();

  const days = new Map<string, { date: Date; events: CalendarEvent[] }>();
  for (const e of data.events) {
    const d = e.all_day ? new Date(`${e.start}T00:00`) : new Date(e.start);
    const key = d.toDateString();
    if (!days.has(key)) days.set(key, { date: d, events: [] });
    days.get(key)!.events.push(e);
  }
  const relative = (d: Date) => {
    const diff = Math.round(
      (new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - today) / 864e5,
    );
    if (diff === 0) return "Today";
    if (diff === 1) return "Tomorrow";
    return d.toLocaleDateString([], { weekday: "long" });
  };

  return (
    <div class="widget agenda">
      {[...days.values()].map(({ date, events }) => (
        <div class="agenda-day" key={date.toDateString()}>
          <div class="agenda-date">
            <span class="agenda-num">{date.getDate()}</span>
            <span class="eyebrow">{date.toLocaleDateString([], { month: "short" })}</span>
          </div>
          <div class="agenda-body">
            <p class="eyebrow eyebrow-accent">{relative(date)}</p>
            <ul>
              {events.map((e, i) => (
                <li key={i}>
                  <span class="agenda-title">{e.title}</span>
                  {!e.all_day && (
                    <span class="agenda-time">
                      {new Date(e.start).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ))}
    </div>
  );
}

// ── App widgets (the Foyer widget format, served by the app itself) ──────

/** A cover or thumbnail that falls back to a title block if it can't load. */
function Thumb({ src, title }: { src?: string; title: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) return <span class="app-thumb-fallback">{title}</span>;
  return <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} />;
}

type AppData = {
  stats: { label: string; value: string; unit?: string; caption?: string; tone?: string }[];
  progress: { label: string; value: number; max: number; caption?: string }[];
  items_title?: string;
  items_layout: "covers" | "list";
  items: {
    title: string;
    subtitle?: string;
    image?: string;
    url?: string;
    progress?: number;
    caption?: string;
    action?: AppAction;
  }[];
};

type AppAction = { label: string; url: string; confirm?: string };

/** Relative item links point into the app's public site. */
function itemHref(url: string | undefined, service: Service): string | undefined {
  if (!url) return undefined;
  if (/^https?:\/\//.test(url)) return url;
  if (!service.url) return undefined;
  try {
    return new URL(url, service.url).toString();
  } catch {
    return undefined;
  }
}

function AppWidget({
  data,
  service,
  onChange,
}: {
  data: AppData;
  service: Service;
  onChange: () => void;
}) {
  const image = (src?: string) =>
    src ? (src.startsWith("/") ? api.widgetImage(service.id, src) : src) : undefined;
  const empty = !data.stats.length && !data.progress.length && !data.items.length;
  if (empty) return <div class="widget widget-empty">Nothing to show yet</div>;

  return (
    <div class="widget app-widget">
      {data.stats.length > 0 && (
        <div class="figures">
          {data.stats.map((s) => (
            <Figure
              key={s.label}
              value={s.value}
              unit={s.unit}
              label={s.label}
              caption={s.caption}
              tone={s.tone}
            />
          ))}
        </div>
      )}
      {data.progress.map((p) => {
        const pct = p.max > 0 ? Math.min(100, (p.value / p.max) * 100) : 0;
        return (
          <div class="app-progress" key={p.label}>
            <div class="app-progress-head">
              <span class="eyebrow">{p.label}</span>
              <span class="app-progress-value">
                {p.value}
                <span>/{p.max}</span>
              </span>
              {p.caption && <span class="app-progress-caption">{p.caption}</span>}
            </div>
            <div class="bar">
              <span style={{ width: `${pct}%` }} />
            </div>
          </div>
        );
      })}
      {data.items.length > 0 && (
        <div class="app-items">
          {data.items_title && <p class="eyebrow eyebrow-accent">{data.items_title}</p>}
          <ul class={data.items_layout === "covers" ? "app-covers" : "app-list"}>
            {data.items.map((it, i) => {
              const href = itemHref(it.url, service);
              const src = image(it.image);
              const body = (
                <>
                  <span class="app-thumb">
                    <Thumb src={src} title={it.title} />
                  </span>
                  <span class="app-item-text">
                    <span class="app-item-title">{it.title}</span>
                    {it.subtitle && <span class="app-item-sub">{it.subtitle}</span>}
                  </span>
                  {it.progress != null && (
                    <span class="app-item-progress">
                      <span class="bar">
                        <span style={{ width: `${it.progress}%` }} />
                      </span>
                      {it.caption && <span class="app-item-caption">{it.caption}</span>}
                    </span>
                  )}
                  {it.progress == null && it.caption && (
                    <span class="app-item-caption">{it.caption}</span>
                  )}
                </>
              );
              const item = href ? (
                <a href={href} target="_blank" rel="noopener noreferrer" class="app-item">
                  {body}
                </a>
              ) : (
                <div class="app-item">{body}</div>
              );
              return (
                <li key={i}>
                  {it.action ? (
                    <ItemAction
                      service={service}
                      action={it.action}
                      title={it.title}
                      onDone={onChange}
                    >
                      {item}
                    </ItemAction>
                  ) : (
                    item
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}

/**
 * A button an app put on an item (Hoist's Deploy on a stack). Foyer runs it
 * against the app and follows it while the app says it's running; the app
 * may be something Foyer itself depends on (a deploy can restart Foyer), so
 * failed polls are retried rather than reported.
 */
function ItemAction(props: {
  service: Service;
  action: AppAction;
  title: string;
  /** Called when the action has finished, to reload the card. */
  onDone: () => void;
  children: ComponentChildren;
}) {
  const { service, action } = props;
  const [confirming, setConfirming] = useState(false);
  const [result, setResult] = useState<ActionResult | null>(null);
  const [error, setError] = useState("");
  const alive = useRef(true);
  useEffect(() => () => void (alive.current = false), []);

  const running = result?.state === "running";

  const follow = async (status: string) => {
    const started = Date.now();
    while (alive.current && Date.now() - started < 60 * 60 * 1000) {
      await new Promise((r) => setTimeout(r, 2500));
      try {
        const next = await api.widgetActionStatus(service.id, status);
        if (!alive.current) return;
        setResult(next);
        if (next.state !== "running") {
          props.onDone();
          return;
        }
      } catch {
        // Foyer or the app may be restarting; keep trying.
      }
    }
  };

  const run = async () => {
    setConfirming(false);
    setError("");
    setResult({ state: "running", message: "Starting…" });
    try {
      const res = await api.widgetAction(service.id, action.url);
      setResult(res);
      if (res.state === "running" && res.status) follow(res.status);
      else props.onDone();
    } catch (e) {
      setResult(null);
      setError((e as Error).message);
    }
  };

  const tone = error || result?.state === "failed" ? "bad" : result?.state === "done" ? "good" : "";
  const message = error || result?.message;
  return (
    <>
      <div class="app-item-row">
        {props.children}
        <button
          class="btn app-action"
          disabled={running}
          onClick={() => (action.confirm ? setConfirming(true) : run())}
        >
          {running && <LoaderCircle size={13} class="spin" />}
          {action.label}
        </button>
      </div>
      {message && (
        <p class={`app-action-note ${tone}`}>
          {message}
          {result?.url && (
            <>
              {" "}
              <a href={result.url} target="_blank" rel="noopener noreferrer">
                Open
              </a>
            </>
          )}
        </p>
      )}
      {confirming && (
        <Dialog
          title={`${action.label}: ${props.title}`}
          onClose={() => setConfirming(false)}
          footer={
            <>
              <span class="spacer" />
              <button class="btn btn-ghost" onClick={() => setConfirming(false)}>
                Cancel
              </button>
              <button class="btn btn-primary" onClick={run}>
                {action.label}
              </button>
            </>
          }
        >
          <p>{action.confirm}</p>
        </Dialog>
      )}
    </>
  );
}
