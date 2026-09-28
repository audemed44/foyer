import { ArrowDown, Download, Eraser, Search, X } from "lucide-preact";
import { memo } from "preact/compat";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { parseAnsi, stripAnsi } from "../ansi";
import { api } from "../api";
import { usePoll } from "../hooks";
import { formatBytes } from "../lib";
import type { DockerContainer, LogLine } from "../types";

const MAX_LINES = 5000;

type Row = LogLine & { id: number; plain: string };
type Conn = "connecting" | "live" | "reconnecting" | "stopped";

/** A full-screen, live log viewer for one container (a Dozzle replacement). */
export function LogViewer({ name, onClose }: { name: string; onClose: () => void }) {
  const [rows, setRows] = useState<Row[]>([]);
  const [conn, setConn] = useState<Conn>("connecting");
  const [follow, setFollow] = useState(true);
  const [wrap, setWrap] = useState(true);
  const [times, setTimes] = useState(false);
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [query, setQuery] = useState("");
  const scroller = useRef<HTMLDivElement>(null);
  const { data: containers } = usePoll<DockerContainer[]>(api.containers, 5000);
  const container = containers?.find((c) => c.name === name);

  // Stream lines in, batching renders to one per animation frame.
  useEffect(() => {
    setRows([]);
    setConn("connecting");
    let nextId = 0;
    let pending: Row[] = [];
    let frame = 0;
    const flush = () => {
      frame = 0;
      const batch = pending;
      pending = [];
      setRows((prev) => {
        const next = prev.concat(batch);
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
      });
    };
    const source = new EventSource(api.logsUrl(name));
    source.onopen = () => setConn("live");
    source.onmessage = (e) => {
      const line = JSON.parse(e.data) as LogLine;
      pending.push({ ...line, id: nextId++, plain: stripAnsi(line.m) });
      if (!frame) frame = requestAnimationFrame(flush);
    };
    source.addEventListener("end", () => {
      setConn("stopped");
      source.close();
    });
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) setConn("stopped");
      else setConn("reconnecting");
    };
    return () => {
      source.close();
      cancelAnimationFrame(frame);
    };
  }, [name]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [onClose]);

  const q = query.trim().toLowerCase();
  const visible = useMemo(
    () =>
      rows.filter(
        (r) => (!errorsOnly || r.s === "err") && (!q || r.plain.toLowerCase().includes(q)),
      ),
    [rows, errorsOnly, q],
  );

  const inner = useRef<HTMLDivElement>(null);
  const followRef = useRef(follow);
  followRef.current = follow;
  // When the user last scrolled by hand; programmatic scrolls and reflow
  // (fonts loading, wrapping) must not switch following off.
  const userScrollAt = useRef(0);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (follow && el) el.scrollTop = el.scrollHeight;
  }, [visible, follow, wrap, times]);

  // Stay pinned to the bottom when the content grows or reflows.
  useEffect(() => {
    const observer = new ResizeObserver(() => {
      const el = scroller.current;
      if (followRef.current && el) el.scrollTop = el.scrollHeight;
    });
    if (inner.current) observer.observe(inner.current);
    if (scroller.current) observer.observe(scroller.current);
    return () => observer.disconnect();
  }, []);

  const markUserScroll = () => {
    userScrollAt.current = Date.now();
  };

  // Scrolling up by hand pauses following; scrolling back to the bottom resumes it.
  const onScroll = () => {
    const el = scroller.current!;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    if (atBottom && !follow) setFollow(true);
    else if (!atBottom && follow && Date.now() - userScrollAt.current < 1000) setFollow(false);
  };

  const download = () => {
    const text = visible.map((r) => (r.t ? `${r.t} ${r.plain}` : r.plain)).join("\n");
    const url = URL.createObjectURL(new Blob([text + "\n"], { type: "text/plain" }));
    const a = Object.assign(document.createElement("a"), { href: url, download: `${name}.log` });
    a.click();
    URL.revokeObjectURL(url);
  };

  const stats = container?.stats;
  const connLabel = {
    connecting: "Connecting",
    live: "Live",
    reconnecting: "Reconnecting",
    stopped: container && container.state !== "running" ? "Container stopped" : "Stream ended",
  }[conn];

  return (
    <div class="logs" role="dialog" aria-modal="true" aria-label={`Logs for ${name}`}>
      <header class="logs-head">
        <div class="logs-title">
          <p class="eyebrow">
            <span class={`dot ${conn === "live" ? "good" : conn === "stopped" ? "" : "warn"}`} />
            {connLabel}
            {container && <span class="logs-image">· {container.image}</span>}
          </p>
          <h2>{name}</h2>
        </div>
        {stats && (
          <div class="logs-stats">
            <LogStat label="CPU" value={stats.cpu == null ? "—" : `${stats.cpu.toFixed(1)}%`} />
            <LogStat label="Memory" value={formatBytes(stats.mem_used)} />
            <LogStat
              label="Net in / out"
              value={`${formatBytes(stats.net_rx)} / ${formatBytes(stats.net_tx)}`}
            />
            <LogStat label="Processes" value={String(stats.pids)} />
          </div>
        )}
        <button
          class="icon-btn logs-close"
          onClick={onClose}
          aria-label="Close logs"
          title="Close (Esc)"
        >
          <X size={20} />
        </button>
      </header>

      <div class="logs-toolbar">
        <label class="logs-search">
          <Search size={15} />
          <input
            value={query}
            onInput={(e) => setQuery(e.currentTarget.value)}
            placeholder="Filter lines"
            spellcheck={false}
          />
          {q && (
            <span class="logs-count">
              {visible.length} / {rows.length}
            </span>
          )}
        </label>
        <Chip on={follow} onClick={() => setFollow(!follow)}>
          Follow
        </Chip>
        <Chip on={wrap} onClick={() => setWrap(!wrap)}>
          Wrap
        </Chip>
        <Chip on={times} onClick={() => setTimes(!times)}>
          Timestamps
        </Chip>
        <Chip on={errorsOnly} onClick={() => setErrorsOnly(!errorsOnly)}>
          Errors only
        </Chip>
        <span class="spacer" />
        <button class="btn btn-ghost" onClick={() => setRows([])} title="Clear the screen">
          <Eraser size={14} /> Clear
        </button>
        <button class="btn btn-ghost" onClick={download} disabled={visible.length === 0}>
          <Download size={14} /> Download
        </button>
      </div>

      <div
        ref={scroller}
        class={`logs-body ${wrap ? "wrap" : ""} ${times ? "with-time" : ""}`}
        onScroll={onScroll}
        onWheel={markUserScroll}
        onTouchMove={markUserScroll}
        onPointerDown={markUserScroll}
        onKeyDown={markUserScroll}
        tabIndex={0}
      >
        <div ref={inner}>
          {visible.length === 0 ? (
            <p class="logs-empty">
              {conn === "connecting"
                ? "Loading logs…"
                : q || errorsOnly
                  ? "No matching lines"
                  : "No output yet"}
            </p>
          ) : (
            visible.map((r) => <LogRow key={r.id} row={r} query={q} showTime={times} />)
          )}
        </div>
      </div>

      {!follow && visible.length > 0 && (
        <button class="logs-jump btn btn-primary" onClick={() => setFollow(true)}>
          <ArrowDown size={14} /> Jump to latest
        </button>
      )}
    </div>
  );
}

function LogStat({ label, value }: { label: string; value: string }) {
  return (
    <div class="logs-stat">
      <p class="eyebrow">{label}</p>
      <p class="logs-stat-value">{value}</p>
    </div>
  );
}

function Chip(props: { on: boolean; onClick: () => void; children: string }) {
  return (
    <button class={`chip ${props.on ? "on" : ""}`} aria-pressed={props.on} onClick={props.onClick}>
      {props.children}
    </button>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return "";
  const time = d.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  const today = new Date().toDateString() === d.toDateString();
  return today ? time : `${d.toLocaleDateString([], { day: "2-digit", month: "short" })} ${time}`;
}

function highlight(text: string, q: string) {
  if (!q) return text;
  const parts: (string | preact.JSX.Element)[] = [];
  const lower = text.toLowerCase();
  let at = 0;
  for (let i = lower.indexOf(q); i !== -1; i = lower.indexOf(q, at)) {
    parts.push(text.slice(at, i), <mark key={i}>{text.slice(i, i + q.length)}</mark>);
    at = i + q.length;
  }
  parts.push(text.slice(at));
  return parts;
}

const LogRow = memo(function LogRow(props: { row: Row; query: string; showTime: boolean }) {
  const { row, query, showTime } = props;
  return (
    <div class={`log-line ${row.s === "err" ? "err" : ""}`}>
      {showTime && <span class="log-time">{row.t ? formatTime(row.t) : ""}</span>}
      <span class="log-text">
        {parseAnsi(row.m).map((seg, i) => (
          <span
            key={i}
            style={seg.fg ? { color: seg.fg } : undefined}
            class={[seg.bold && "b", seg.dim && "d", seg.italic && "i", seg.underline && "u"]
              .filter(Boolean)
              .join(" ")}
          >
            {highlight(seg.text, query)}
          </span>
        ))}
      </span>
    </div>
  );
});
