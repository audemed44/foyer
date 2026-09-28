import { api } from "../api";
import { usePoll, useNow } from "../hooks";
import { formatBytes, formatDuration, greeting } from "../lib";
import type { Config, SystemStats } from "../types";

export function Header({ config }: { config: Config }) {
  const { header } = config;
  const now = useNow();
  const time = now.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    hour12: !header.clock_24h,
  });
  const date = now.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" });
  const hello = header.greeting
    ? `${greeting(now.getHours())}${header.name ? `, ${header.name}` : ""}`
    : null;

  return (
    <header class="masthead">
      <div class="masthead-time">
        {header.clock && <div class="clock">{time}</div>}
        <div class="dateline">
          <span>{date}</span>
          {hello && <span class="dateline-greeting">{hello}</span>}
        </div>
      </div>
      {header.system.enabled && <SystemMeters config={config} />}
    </header>
  );
}

function SystemMeters({ config }: { config: Config }) {
  const opts = config.header.system;
  const { data } = usePoll<SystemStats>(api.system, 5000);
  if (!data) return <div class="meters meters-loading" />;

  return (
    <div class="meters">
      {opts.cpu && (
        <Meter
          label="CPU"
          value={`${Math.round(data.cpu.percent)}%`}
          percent={data.cpu.percent}
          history={data.cpu.history}
          detail={data.cpu.load ? `load ${data.cpu.load[0].toFixed(2)}` : `${data.cpu.cores} cores`}
        />
      )}
      {opts.memory && (
        <Meter
          label="MEM"
          value={`${Math.round(data.memory.percent)}%`}
          percent={data.memory.percent}
          history={data.memory.history}
          detail={`${formatBytes(data.memory.used)} / ${formatBytes(data.memory.total)}`}
        />
      )}
      {data.disks.map((d) => (
        <Meter
          key={d.path}
          label={d.path === "/" ? "DISK" : d.path.split("/").filter(Boolean).pop()!.toUpperCase()}
          value={`${Math.round(d.percent)}%`}
          percent={d.percent}
          detail={`${formatBytes(d.total - d.used)} free`}
        />
      ))}
      {opts.temperature && data.temperature !== null && (
        <Meter
          label="TEMP"
          value={`${Math.round(data.temperature)}°`}
          percent={Math.min(100, data.temperature)}
          detail={data.temperature >= 80 ? "running hot" : "cpu package"}
          warnAt={75}
        />
      )}
      {opts.uptime && (
        <div class="meter">
          <div class="meter-label">UP</div>
          <div class="meter-value">{formatDuration(data.uptime)}</div>
          <div class="meter-detail">since boot</div>
        </div>
      )}
    </div>
  );
}

function Meter(props: {
  label: string;
  value: string;
  percent: number;
  detail: string;
  history?: number[];
  warnAt?: number;
}) {
  const warnAt = props.warnAt ?? 85;
  const tone = props.percent >= warnAt + 10 ? "bad" : props.percent >= warnAt ? "warn" : "";
  return (
    <div class={`meter ${tone}`}>
      <div class="meter-label">{props.label}</div>
      <div class="meter-value">{props.value}</div>
      {props.history && props.history.length > 1 ? (
        <Sparkline values={props.history} />
      ) : (
        <div class="meter-bar">
          <span style={{ width: `${Math.max(2, props.percent)}%` }} />
        </div>
      )}
      <div class="meter-detail">{props.detail}</div>
    </div>
  );
}

function Sparkline({ values }: { values: number[] }) {
  const w = 100;
  const h = 18;
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
