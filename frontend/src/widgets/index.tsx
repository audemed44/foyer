import { ArrowDown, ArrowUp, Timer } from "lucide-preact";
import type { ComponentChildren } from "preact";
import { api } from "../api";
import { usePoll, useNow } from "../hooks";
import { timeAgo } from "../lib";
import type { Service } from "../types";

export function WidgetBody({ service }: { service: Service }) {
  const { data, error } = usePoll<unknown>(() => api.widget(service.id), 60000, [service.id]);
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

function Figure(props: {
  value: string;
  label: string;
  unit?: string;
  tone?: string;
  icon?: ComponentChildren;
}) {
  return (
    <div class={`figure ${props.tone ?? ""}`}>
      <p class="figure-value">
        {props.value}
        {props.unit && <span class="figure-unit">{props.unit}</span>}
      </p>
      <p class="eyebrow figure-label">
        {props.icon}
        {props.label}
      </p>
    </div>
  );
}
