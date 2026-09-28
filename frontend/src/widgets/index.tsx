import { ArrowDown, ArrowUp, Timer } from "lucide-preact";
import { api } from "../api";
import { usePoll, useNow } from "../hooks";
import { timeAgo } from "../lib";
import type { Service } from "../types";

export function WidgetBody({ service }: { service: Service }) {
  const { data, error } = usePoll<unknown>(() => api.widget(service.id), 60000, [service.id]);
  const type = service.widget?.type;

  if (error && !data) return <div class="widget widget-error">{error}</div>;
  if (!data) return <div class="widget widget-loading" />;
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
  return (
    <div class="widget kuma">
      <div class="stat-row">
        <Stat value={`${data.up}/${data.total}`} label="up" tone={data.down ? "bad" : "good"} />
        {data.uptime !== undefined && (
          <Stat
            value={`${data.uptime.toFixed(data.uptime >= 99.95 ? 0 : 2)}%`}
            label="24h uptime"
          />
        )}
        {data.down > 0 && <Stat value={String(data.down)} label="down" tone="bad" />}
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
              {m.history.slice(-20).map((b, i) => (
                <i key={i} class={b} />
              ))}
            </span>
            <span class="kuma-ping">{m.ping != null ? `${m.ping}ms` : ""}</span>
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
      <div class="stat-row">
        <Stat
          value={fmt(data.download_mbps)}
          unit="Mbps"
          label="down"
          icon={<ArrowDown size={12} />}
        />
        <Stat value={fmt(data.upload_mbps)} unit="Mbps" label="up" icon={<ArrowUp size={12} />} />
        <Stat value={data.ping_ms.toFixed(0)} unit="ms" label="ping" icon={<Timer size={12} />} />
      </div>
      {at && !isNaN(at.getTime()) && <div class="widget-foot">Tested {timeAgo(at, now)}</div>}
    </div>
  );
}

type CalendarEvent = { title: string; start: string; all_day: boolean };
type CalendarData = { events: CalendarEvent[] };

function Calendar({ data }: { data: CalendarData }) {
  const now = useNow();
  if (data.events.length === 0) return <div class="widget widget-empty">Nothing coming up</div>;
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const dayLabel = (d: Date) => {
    const diff = Math.round(
      (new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - today.getTime()) / 864e5,
    );
    if (diff === 0) return "Today";
    if (diff === 1) return "Tomorrow";
    return d.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" });
  };
  const days = new Map<string, { label: string; events: CalendarEvent[] }>();
  for (const e of data.events) {
    const d = e.all_day ? new Date(`${e.start}T00:00`) : new Date(e.start);
    const label = dayLabel(d);
    if (!days.has(label)) days.set(label, { label, events: [] });
    days.get(label)!.events.push(e);
  }
  return (
    <div class="widget agenda">
      {[...days.values()].map((day) => (
        <div class="agenda-day" key={day.label}>
          <div class="agenda-date">{day.label}</div>
          <ul>
            {day.events.map((e, i) => (
              <li key={i}>
                <span class="agenda-time">
                  {e.all_day
                    ? "all day"
                    : new Date(e.start).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                </span>
                <span class="agenda-title">{e.title}</span>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function Stat(props: {
  value: string;
  label: string;
  unit?: string;
  tone?: string;
  icon?: preact.ComponentChildren;
}) {
  return (
    <div class={`stat ${props.tone ?? ""}`}>
      <div class="stat-value">
        {props.value}
        {props.unit && <span class="stat-unit">{props.unit}</span>}
      </div>
      <div class="stat-label">
        {props.icon}
        {props.label}
      </div>
    </div>
  );
}
