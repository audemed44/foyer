import { ScrollText, Search } from "lucide-preact";
import { useMemo, useState } from "preact/hooks";
import { api } from "../api";
import { usePoll } from "../hooks";
import { formatBytes } from "../lib";
import type { DockerContainer, SystemStats } from "../types";
import { Segmented, Toggle } from "./ui";

type Sort = "name" | "cpu" | "memory";

/** Every container on the host with live resource usage (a `docker stats` page). */
export function ContainersPage({ onLogs }: { onLogs: (name: string) => void }) {
  const { data, error } = usePoll<DockerContainer[]>(api.containers, 5000);
  const { data: system } = usePoll<SystemStats>(api.system, 15000);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<Sort>("name");
  const [showStopped, setShowStopped] = useState(true);

  const all = data ?? [];
  const running = all.filter((c) => c.state === "running");
  const memTotal = running.reduce((n, c) => n + (c.stats?.mem_used ?? 0), 0);
  const cpuTotal = running.reduce((n, c) => n + (c.stats?.cpu ?? 0), 0);
  const unhealthy = all.filter((c) => c.health === "unhealthy").length;
  const hostMem = system?.memory.total ?? 0;

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = all
      .filter((c) => showStopped || c.state === "running")
      .filter((c) => !q || c.name.toLowerCase().includes(q) || c.image.toLowerCase().includes(q))
      .sort((a, b) => {
        if (sort === "cpu") return (b.stats?.cpu ?? -1) - (a.stats?.cpu ?? -1);
        if (sort === "memory") return (b.stats?.mem_used ?? 0) - (a.stats?.mem_used ?? 0);
        return a.name.localeCompare(b.name);
      });
    const byProject = new Map<string, DockerContainer[]>();
    for (const c of list) {
      const key = c.project || "Standalone";
      if (!byProject.has(key)) byProject.set(key, []);
      byProject.get(key)!.push(c);
    }
    // Largest projects first; "Standalone" last.
    return [...byProject.entries()].sort(
      (a, b) =>
        Number(a[0] === "Standalone") - Number(b[0] === "Standalone") || b[1].length - a[1].length,
    );
  }, [all, query, sort, showStopped]);

  if (error && !data) {
    return (
      <section class="ct-page">
        <PageTitle />
        <div class="empty">
          {error}. Mount <code>/var/run/docker.sock</code> into the Foyer container to see your
          containers here.
        </div>
      </section>
    );
  }

  return (
    <section class="ct-page">
      <PageTitle />
      <div class="figures page-figures stagger">
        <Figure value={String(running.length)} unit={`/${all.length}`} label="Running" />
        <Figure value={cpuTotal.toFixed(0)} unit="%" label="CPU · all containers" />
        <Figure
          value={formatBytes(memTotal).split(" ")[0]}
          unit={formatBytes(memTotal).split(" ")[1]}
          label={hostMem ? `Memory · ${Math.round((memTotal / hostMem) * 100)}% of host` : "Memory"}
        />
        <Figure value={String(unhealthy)} label="Unhealthy" tone={unhealthy ? "bad" : undefined} />
      </div>

      <div class="page-controls">
        <label class="logs-search">
          <Search size={15} />
          <input
            value={query}
            onInput={(e) => setQuery(e.currentTarget.value)}
            placeholder="Filter containers"
            spellcheck={false}
          />
        </label>
        <Segmented
          value={sort}
          options={[
            ["name", "Name"],
            ["cpu", "CPU"],
            ["memory", "Memory"],
          ]}
          onChange={setSort}
        />
        <Toggle label="Show stopped" checked={showStopped} onChange={setShowStopped} />
      </div>

      {!data && <div class="widget-loading skeleton ct-skeleton" />}
      {groups.map(([project, list], gi) => (
        <div class="ct-group" key={project}>
          <div class="group-head">
            <div class="group-title">
              <span class="group-index">{String(gi + 1).padStart(2, "0")}</span>
              <h2 class="group-name">{project}</h2>
            </div>
            <span class="spacer" />
            <span class="eyebrow">
              {list.filter((c) => c.state === "running").length} of {list.length} running
            </span>
          </div>
          <div class="ct-list stagger">
            {list.map((c) => (
              <ContainerRow key={c.id} c={c} hostMem={hostMem} onLogs={() => onLogs(c.name)} />
            ))}
          </div>
        </div>
      ))}
      {data && groups.length === 0 && <div class="empty">No containers match.</div>}
    </section>
  );
}

function PageTitle() {
  return (
    <header class="page-head">
      <p class="eyebrow eyebrow-accent">Docker</p>
      <h1 class="page-title">Containers</h1>
    </header>
  );
}

function tone(c: DockerContainer): string {
  if (c.state === "restarting") return "warn";
  if (c.state !== "running")
    return c.state === "exited" && c.status.startsWith("Exited (0)") ? "" : "bad";
  if (c.health === "unhealthy") return "bad";
  if (c.health === "starting") return "warn";
  return "good";
}

function ContainerRow(props: { c: DockerContainer; hostMem: number; onLogs: () => void }) {
  const { c, hostMem } = props;
  const s = c.stats;
  const cpu = s?.cpu ?? null;
  // A container without a memory limit reports the host's memory as its limit.
  const memCap = s && s.mem_limit && s.mem_limit < hostMem * 0.98 ? s.mem_limit : hostMem;
  const memPct = s && memCap ? (s.mem_used / memCap) * 100 : 0;
  const running = c.state === "running";

  return (
    <div class={`ct-row ${running ? "" : "ct-stopped"}`}>
      <button class="ct-main" onClick={props.onLogs} title="Open logs">
        <span class={`dot ${tone(c)}`} />
        <span class="ct-name-wrap">
          <span class="ct-name">{c.name}</span>
          <span class="ct-image">{c.image}</span>
        </span>
      </button>
      <span class="ct-status">{c.status}</span>
      <Meter
        label="CPU"
        value={running && cpu !== null ? `${cpu.toFixed(1)}%` : "—"}
        pct={cpu ?? 0}
      />
      <Meter
        label={memCap !== hostMem ? `Mem · limit ${formatBytes(memCap)}` : "Memory"}
        value={running && s ? formatBytes(s.mem_used) : "—"}
        pct={memPct}
      />
      <span class="ct-net">
        <span class="eyebrow">Net</span>
        <span>{running && s ? `↓ ${formatBytes(s.net_rx)}  ↑ ${formatBytes(s.net_tx)}` : "—"}</span>
      </span>
      <button class="btn ct-logs" onClick={props.onLogs}>
        <ScrollText size={14} /> Logs
      </button>
    </div>
  );
}

function Meter({ label, value, pct }: { label: string; value: string; pct: number }) {
  const level = pct >= 90 ? "bad" : pct >= 70 ? "warn" : "";
  return (
    <span class={`ct-meter ${level}`}>
      <span class="eyebrow">{label}</span>
      <span class="ct-meter-value">{value}</span>
      <span class="bar">
        <span style={{ width: `${Math.min(100, Math.max(pct > 0 ? 1 : 0, pct))}%` }} />
      </span>
    </span>
  );
}

function Figure(props: { value: string; label: string; unit?: string; tone?: string }) {
  return (
    <div class={`figure ${props.tone ?? ""}`}>
      <p class="figure-value">
        {props.value}
        {props.unit && <span class="figure-unit">{props.unit}</span>}
      </p>
      <p class="eyebrow figure-label">{props.label}</p>
    </div>
  );
}
