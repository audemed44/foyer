import { Activity, Clock3, Cpu, HardDrive, MemoryStick, Thermometer } from "lucide-preact";
import type { ComponentChildren } from "preact";
import { api } from "../api";
import { usePoll, useNow } from "../hooks";
import { formatBytes, formatDuration, greeting } from "../lib";
import type { Config, StatusMap, SystemStats } from "../types";

export function Header({ config, status }: { config: Config; status: StatusMap | null }) {
  const { header } = config;
  const now = useNow();
  const parts = new Intl.DateTimeFormat([], {
    hour: "2-digit",
    minute: "2-digit",
    hour12: !header.clock_24h,
  }).formatToParts(now);
  const hour = parts.find((p) => p.type === "hour")?.value ?? "";
  const minute = parts.find((p) => p.type === "minute")?.value ?? "";
  const period = parts.find((p) => p.type === "dayPeriod")?.value;
  const date = now.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" });

  return (
    <header class="hero">
      <div class="hero-lead">
        <p class="eyebrow eyebrow-accent">{date}</p>
        {header.clock && (
          <h1 class="clock" aria-label={`${hour}:${minute}${period ? ` ${period}` : ""}`}>
            {hour}
            <span class="clock-colon">:</span>
            {minute}
            {period && <span class="clock-period">{period}</span>}
          </h1>
        )}
        {header.greeting && (
          <p class="greeting">
            {greeting(now.getHours())}
            {header.name ? `, ${header.name}` : ""}.
          </p>
        )}
      </div>
      {header.system.enabled && <Stats config={config} status={status} />}
    </header>
  );
}

function Stats({ config, status }: { config: Config; status: StatusMap | null }) {
  const opts = config.header.system;
  const { data } = usePoll<SystemStats>(api.system, 5000);

  const pings = Object.values(status ?? {}).flatMap((s) => (s.ping ? [s.ping] : []));
  const up = pings.filter((p) => p.state === "up").length;

  const tiles: ComponentChildren[] = [];
  if (pings.length > 0) {
    tiles.push(
      <Stat
        key="services"
        icon={<Activity size={12} />}
        label="Online"
        value={String(up)}
        unit={`/${pings.length}`}
        sub={up === pings.length ? "all services up" : `${pings.length - up} down`}
        tone={up === pings.length ? "" : "bad"}
      />,
    );
  }
  if (data) {
    if (opts.cpu)
      tiles.push(
        <Stat
          key="cpu"
          icon={<Cpu size={12} />}
          label="CPU"
          value={String(Math.round(data.cpu.percent))}
          unit="%"
          sub={data.cpu.load ? `load ${data.cpu.load[0].toFixed(2)}` : `${data.cpu.cores} cores`}
          history={data.cpu.history}
          percent={data.cpu.percent}
        />,
      );
    if (opts.memory)
      tiles.push(
        <Stat
          key="mem"
          icon={<MemoryStick size={12} />}
          label="Memory"
          value={String(Math.round(data.memory.percent))}
          unit="%"
          sub={`${formatBytes(data.memory.used)} of ${formatBytes(data.memory.total)}`}
          history={data.memory.history}
          percent={data.memory.percent}
        />,
      );
    for (const d of data.disks)
      tiles.push(
        <Stat
          key={d.path}
          icon={<HardDrive size={12} />}
          label={d.path === "/" ? "Disk" : d.path.split("/").filter(Boolean).pop()!}
          value={String(Math.round(d.percent))}
          unit="%"
          sub={`${formatBytes(d.total - d.used)} free`}
          percent={d.percent}
        />,
      );
    if (opts.temperature && data.temperature !== null)
      tiles.push(
        <Stat
          key="temp"
          icon={<Thermometer size={12} />}
          label="Temp"
          value={String(Math.round(data.temperature))}
          unit="°C"
          sub={data.temperature >= 80 ? "running hot" : "cpu package"}
          percent={data.temperature}
          warnAt={75}
        />,
      );
    if (opts.uptime) {
      const [big, small] = formatDuration(data.uptime).split(" ");
      tiles.push(
        <Stat
          key="up"
          icon={<Clock3 size={12} />}
          label="Uptime"
          value={big}
          unit={small ? ` ${small}` : ""}
          sub="since boot"
        />,
      );
    }
  }

  return <div class={`stats ${data ? "stagger" : "stats-loading"}`}>{tiles}</div>;
}

function Stat(props: {
  icon: ComponentChildren;
  label: string;
  value: string;
  unit?: string;
  sub: string;
  history?: number[];
  percent?: number;
  warnAt?: number;
  tone?: string;
}) {
  const warnAt = props.warnAt ?? 85;
  const pct = props.percent;
  const tone =
    props.tone ??
    (pct === undefined ? "" : pct >= warnAt + 10 ? "bad" : pct >= warnAt ? "warn" : "");
  return (
    <div class={`stat ${tone}`}>
      <p class="eyebrow stat-label">
        {props.icon}
        {props.label}
      </p>
      <p class="stat-value">
        {props.value}
        {props.unit && <span class="stat-unit">{props.unit}</span>}
      </p>
      {props.history && props.history.length > 1 ? (
        <Sparkline values={props.history} />
      ) : pct !== undefined ? (
        <div class="bar">
          <span style={{ width: `${Math.max(1, Math.min(100, pct))}%` }} />
        </div>
      ) : (
        <div class="bar bar-empty" />
      )}
      <p class="stat-sub">{props.sub}</p>
    </div>
  );
}

function Sparkline({ values }: { values: number[] }) {
  const w = 120;
  const h = 22;
  const step = w / Math.max(1, values.length - 1);
  const y = (v: number) => h - (Math.min(100, Math.max(0, v)) / 100) * (h - 2) - 1;
  const points = values.map((v, i) => `${(i * step).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  return (
    <svg class="sparkline" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <polygon points={`0,${h} ${points} ${w},${h}`} class="sparkline-fill" />
      <polyline points={points} class="sparkline-line" />
    </svg>
  );
}
