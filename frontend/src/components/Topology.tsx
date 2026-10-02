import {
  AlertTriangle,
  ArrowUpRight,
  Globe,
  HardDrive,
  Lock,
  RefreshCw,
  ScrollText,
  Search,
  Server,
  X,
} from "lucide-preact";
import type { ComponentChildren } from "preact";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { api } from "../api";
import { useMedia, usePoll, useNow } from "../hooks";
import { timeAgo } from "../lib";
import { chain, layout, protection, shortPath, type Filters } from "../topology";
import type { TopoContainer, TopoDomain, TopoStorage, Topology } from "../types";
import { Figure } from "../widgets/figure";
import { Icon } from "./Icon";
import { Toggle } from "./ui";

/**
 * The homelab as a map: domains (Nginx Proxy Manager hosts) on the left,
 * the containers they reach in the middle, and the folders and volumes those
 * containers keep their data in on the right, with Kopia and Syncthing
 * coverage.
 */
export function TopologyPage({ onLogs }: { onLogs: (name: string) => void }) {
  const { data, error } = usePoll<Topology>(api.topology, 30000);
  const [filters, setFilters] = useState<Filters>({ query: "", system: false, stopped: true });
  const [hover, setHover] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const narrow = useMedia("(max-width: 900px)");

  const view = useMemo(() => (data ? layout(data, filters) : null), [data, filters]);
  const focus = selected ?? hover;
  const lit = useMemo(() => (data && focus ? chain(data, focus) : null), [data, focus]);

  if (error && !data) {
    return (
      <section class="topo-page">
        <PageTitle />
        <div class="empty">{error}</div>
      </section>
    );
  }
  if (!data || !view) {
    return (
      <section class="topo-page">
        <PageTitle />
        <div class="widget-loading skeleton topo-skeleton" />
      </section>
    );
  }

  const kopia = data.sources.kopia !== undefined;
  const running = data.containers.filter((c) => c.state === "running").length;
  const dataStores = data.storage.filter((s) => s.class === "data" && s.written);
  const protectedCount = dataStores.filter((s) => s.backup && !s.backup.partial).length;
  const select = (id: string | null) => setSelected((cur) => (cur === id ? null : id));

  return (
    <section class="topo-page">
      <PageTitle />
      <div class="figures page-figures stagger">
        <Figure
          value={String(data.domains.length)}
          label="Domains"
          caption={data.sources.npm === undefined ? "Add a Gatehouse widget" : undefined}
        />
        <Figure
          value={String(running)}
          unit={`/${data.containers.length}`}
          label="Containers running"
        />
        {kopia ? (
          <Figure
            value={String(protectedCount)}
            unit={`/${dataStores.length}`}
            label="Data folders backed up"
            tone={protectedCount < dataStores.length ? "warn" : undefined}
          />
        ) : (
          <Figure value={String(dataStores.length)} label="Data folders" />
        )}
        <Figure
          value={String(data.issues.length)}
          label={data.issues.length === 1 ? "Issue" : "Issues"}
          tone={data.issues.some((i) => i.tone === "bad") ? "bad" : undefined}
        />
      </div>

      <SourceNotes sources={data.sources} />

      {data.issues.length > 0 && (
        <ol class="topo-issues">
          {data.issues.map((issue, i) => (
            <li key={i} class={issue.tone}>
              <AlertTriangle size={14} />
              {issue.node ? (
                <button onClick={() => select(issue.node!)}>{issue.text}</button>
              ) : (
                <span>{issue.text}</span>
              )}
            </li>
          ))}
        </ol>
      )}

      <div class="page-controls">
        <label class="logs-search">
          <Search size={15} />
          <input
            value={filters.query}
            onInput={(e) => setFilters({ ...filters, query: e.currentTarget.value })}
            placeholder="Find a domain, container or path"
            spellcheck={false}
          />
        </label>
        <Toggle
          label="Stopped containers"
          checked={filters.stopped}
          onChange={(v) => setFilters({ ...filters, stopped: v })}
        />
        <Toggle
          label="System mounts"
          checked={filters.system}
          onChange={(v) => setFilters({ ...filters, system: v })}
        />
      </div>

      {selected && (
        <Inspector id={selected} data={data} onClose={() => setSelected(null)} onLogs={onLogs} />
      )}

      {narrow ? (
        <StackedView data={data} view={view} kopia={kopia} onSelect={select} />
      ) : (
        <MapView
          data={data}
          view={view}
          kopia={kopia}
          lit={lit}
          selected={selected}
          onHover={setHover}
          onSelect={select}
        />
      )}
    </section>
  );
}

function PageTitle() {
  return (
    <header class="page-head">
      <p class="eyebrow eyebrow-accent">Architecture</p>
      <h1 class="page-title">Topology</h1>
    </header>
  );
}

function SourceNotes({ sources }: { sources: Topology["sources"] }) {
  const notes: string[] = [];
  const failed = (name: string, err?: string) => err && notes.push(`${name}: ${err}`);
  failed("Docker", sources.docker);
  failed("Reverse proxy", sources.npm);
  failed("Kopia", sources.kopia);
  failed("Syncthing", sources.syncthing);
  if (sources.docker === undefined) notes.push("Mount the Docker socket to see containers.");
  if (sources.npm === undefined)
    notes.push("Add a Gatehouse (or Nginx Proxy Manager) widget to a service to map your domains.");
  if (sources.kopia === undefined) notes.push("Add a Kopia widget to see which data is backed up.");
  if (!notes.length) return null;
  return (
    <ul class="topo-notes">
      {notes.map((n) => (
        <li key={n}>{n}</li>
      ))}
    </ul>
  );
}

// ── Map (desktop) ────────────────────────────────────────────────────────

type ViewProps = {
  data: Topology;
  view: ReturnType<typeof layout>;
  kopia: boolean;
};

function MapView(
  props: ViewProps & {
    lit: Set<string> | null;
    selected: string | null;
    onHover: (id: string | null) => void;
    onSelect: (id: string) => void;
  },
) {
  const { data, view, lit } = props;
  const map = useRef<HTMLDivElement>(null);
  const [paths, setPaths] = useState<{ key: string; d: string; from: string; to: string }[]>([]);
  const [size, setSize] = useState({ w: 0, h: 0 });

  // Draw a curve from the right edge of each node to the left edge of the
  // node it links to, re-measured whenever the layout can have moved.
  useLayoutEffect(() => {
    const el = map.current;
    if (!el) return;
    const measure = () => {
      const box = el.getBoundingClientRect();
      const rects = new Map<string, DOMRect>();
      el.querySelectorAll<HTMLElement>("[data-node]").forEach((n) =>
        rects.set(n.dataset.node!, n.getBoundingClientRect()),
      );
      const next: typeof paths = [];
      for (const l of data.links) {
        const a = rects.get(l.from);
        const b = rects.get(l.to);
        if (!a || !b) continue;
        const x1 = a.right - box.left;
        const y1 = a.top + a.height / 2 - box.top;
        const x2 = b.left - box.left;
        const y2 = b.top + b.height / 2 - box.top;
        const dx = (x2 - x1) / 2;
        next.push({
          key: `${l.from}>${l.to}>${l.label}`,
          d: `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`,
          from: l.from,
          to: l.to,
        });
      }
      setPaths(next);
      setSize({ w: box.width, h: box.height });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    document.fonts?.ready.then(measure);
    return () => observer.disconnect();
  }, [data, view]);

  const state = (id: string) => (lit ? (lit.has(id) ? "lit" : "dim") : "");
  const nodeProps = (id: string) => ({
    "data-node": id,
    class: `topo-node ${state(id)} ${props.selected === id ? "selected" : ""}`,
    onMouseEnter: () => props.onHover(id),
    onMouseLeave: () => props.onHover(null),
    onClick: () => props.onSelect(id),
  });

  return (
    <div class="topo-map" ref={map}>
      <svg class="topo-links" width={size.w} height={size.h} aria-hidden="true">
        {paths.map((p) => (
          <path
            key={p.key}
            d={p.d}
            class={lit ? (lit.has(p.from) && lit.has(p.to) ? "lit" : "dim") : ""}
          />
        ))}
      </svg>

      <div class="topo-col">
        <ColumnHead n={1} title="Domains" count={view.domains.length} />
        {view.domains.map((d) => (
          <button key={d.id} {...nodeProps(d.id)}>
            <DomainBody d={d} />
          </button>
        ))}
        {view.domains.length === 0 && <p class="topo-empty">No proxy hosts</p>}
      </div>

      <div class="topo-col">
        <ColumnHead
          n={2}
          title="Containers"
          count={view.groups.reduce((n, g) => n + g.containers.length, 0)}
        />
        {data.host && view.visible.has("host") && (
          <button {...nodeProps("host")}>
            <HostBody ports={data.host.ports} />
          </button>
        )}
        {view.groups.map((g) => (
          <div class="topo-group" key={g.project}>
            <p class="eyebrow topo-group-name">{g.project}</p>
            {g.containers.map((c) => (
              <button key={c.id} {...nodeProps(c.id)}>
                <ContainerBody c={c} />
              </button>
            ))}
          </div>
        ))}
      </div>

      <div class="topo-col">
        <ColumnHead n={3} title="Storage" count={view.storage.length} />
        {view.storage.map((s) => (
          <button key={s.id} {...nodeProps(s.id)}>
            <StorageBody s={s} kopia={props.kopia} />
          </button>
        ))}
      </div>
    </div>
  );
}

function ColumnHead({ n, title, count }: { n: number; title: string; count: number }) {
  return (
    <div class="group-head topo-col-head">
      <div class="group-title">
        <span class="group-index">{String(n).padStart(2, "0")}</span>
        <h2 class="group-name">{title}</h2>
      </div>
      <span class="spacer" />
      <span class="eyebrow">{count}</span>
    </div>
  );
}

function stateTone(state: string, health?: string): string {
  if (state === "restarting") return "warn";
  if (state !== "running") return "bad";
  if (health === "unhealthy") return "bad";
  if (health === "starting") return "warn";
  return "good";
}

/** Container icons: the dashboard service's, or a guess from the image. */
function containerIcon(c: TopoContainer): string {
  if (c.service?.icon) return c.service.icon;
  if (c.image.startsWith("sha256:")) return `${c.name}.png`;
  const name = c.image.split("@")[0].split("/").pop()!.split(":")[0].toLowerCase();
  return `${name}.png`;
}

function DomainBody({ d }: { d: TopoDomain }) {
  const tone = !d.enabled ? "" : d.sleep ? "accent" : !d.target || d.error ? "bad" : "good";
  const cert =
    d.cert_days !== undefined && d.cert_days < 14 ? (
      <span class={`topo-badge ${d.cert_days < 3 ? "bad" : "warn"}`}>cert {d.cert_days}d</span>
    ) : null;
  return (
    <>
      <span class="topo-icon">
        {d.service ? (
          <Icon icon={d.service.icon} name={d.service.name} size={18} />
        ) : (
          <Globe size={15} />
        )}
      </span>
      <span class="topo-text">
        <span class="topo-name">
          {d.ssl && <Lock size={11} class="topo-lock" />}
          {d.name}
        </span>
        <span class="topo-sub">
          {d.enabled ? d.forward.replace(/^https?:\/\//, "→ ") : "disabled"}
        </span>
      </span>
      {d.sleep && <span class="topo-badge">{d.sleep}</span>}
      {cert}
      <span class={`dot ${tone}`} />
    </>
  );
}

function HostBody({ ports }: { ports: number[] }) {
  return (
    <>
      <span class="topo-icon">
        <Server size={15} />
      </span>
      <span class="topo-text">
        <span class="topo-name">This host</span>
        <span class="topo-sub">not in Docker · port {ports.join(", ")}</span>
      </span>
    </>
  );
}

function ContainerBody({ c }: { c: TopoContainer }) {
  const ports = (c.published ?? []).map((p) => p.host).join(", ");
  return (
    <>
      <span class="topo-icon">
        <Icon icon={containerIcon(c)} name={c.service?.name ?? c.name} size={20} />
      </span>
      <span class="topo-text">
        <span class="topo-name">{c.service?.name ?? c.name}</span>
        <span class="topo-sub">
          {c.service ? `${c.name} · ` : ""}
          {c.image.split("@")[0]}
        </span>
      </span>
      {ports && <span class="topo-ports">:{ports}</span>}
      <span class={`dot ${stateTone(c.state, c.health)}`} title={c.status} />
    </>
  );
}

function StorageBody({ s, kopia }: { s: TopoStorage; kopia: boolean }) {
  const now = useNow();
  const p = protection(s, kopia);
  return (
    <>
      <span class="topo-icon">
        <HardDrive size={15} />
      </span>
      <span class="topo-text">
        <span class="topo-name" title={s.path}>
          {s.kind === "volume" ? s.name : shortPath(s.path)}
        </span>
        <span class="topo-sub">
          {s.kind === "volume" ? "volume" : s.class === "socket" ? "Docker socket" : "folder"}
          {s.backup?.last && !s.backup.partial
            ? ` · backed up ${timeAgo(new Date(s.backup.last), now)}`
            : ""}
        </span>
      </span>
      {s.sync && (
        <span class="topo-badge accent" title={`Syncthing folder “${s.sync.folder}”`}>
          <RefreshCw size={10} /> {s.sync.folder}
        </span>
      )}
      {p && <span class={`topo-badge ${p.tone}`}>{p.label}</span>}
    </>
  );
}

// ── Stacked (phone) ──────────────────────────────────────────────────────

function StackedView(props: ViewProps & { onSelect: (id: string) => void }) {
  const { data, view } = props;
  const domainsFor = (id: string) => data.domains.filter((d) => d.target === id);
  const storageFor = (id: string) =>
    data.links
      .filter((l) => l.from === id && view.visible.has(l.to))
      .map((l) => ({ link: l, s: data.storage.find((s) => s.id === l.to)! }))
      .filter((x) => x.s);
  const orphans = view.domains.filter((d) => !d.target);
  return (
    <div class="topo-stack">
      {data.host && (
        <div class="topo-card">
          <div class="topo-card-head topo-node">
            <HostBody ports={data.host.ports} />
          </div>
          {domainsFor("host").map((d) => (
            <div class="topo-card-row" key={d.id}>
              <DomainBody d={d} />
            </div>
          ))}
        </div>
      )}
      {view.groups.map((g) => (
        <div key={g.project} class="topo-stack-group">
          <p class="eyebrow">{g.project}</p>
          {g.containers.map((c) => (
            <div class="topo-card" key={c.id}>
              <button class="topo-card-head topo-node" onClick={() => props.onSelect(c.id)}>
                <ContainerBody c={c} />
              </button>
              {domainsFor(c.id).map((d) => (
                <div class="topo-card-row" key={d.id}>
                  <DomainBody d={d} />
                </div>
              ))}
              {storageFor(c.id).map(({ link, s }) => (
                <div class="topo-card-row" key={s.id + link.label}>
                  <StorageBody s={s} kopia={props.kopia} />
                </div>
              ))}
            </div>
          ))}
        </div>
      ))}
      {orphans.length > 0 && (
        <div class="topo-card">
          <p class="eyebrow topo-card-title">Domains with nothing behind them</p>
          {orphans.map((d) => (
            <div class="topo-card-row" key={d.id}>
              <DomainBody d={d} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Inspector ────────────────────────────────────────────────────────────

function Inspector(props: {
  id: string;
  data: Topology;
  onClose: () => void;
  onLogs: (name: string) => void;
}) {
  const { id, data } = props;
  const now = useNow();
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => panel.current?.scrollIntoView({ block: "nearest", behavior: "smooth" }), [id]);

  const incoming = data.links.filter((l) => l.to === id);
  const outgoing = data.links.filter((l) => l.from === id);
  const name = (ref: string) =>
    data.domains.find((d) => d.id === ref)?.name ??
    data.containers.find((c) => c.id === ref)?.name ??
    (ref === "host" ? "This host" : undefined) ??
    shortPath(data.storage.find((s) => s.id === ref)?.path ?? ref);

  let title = name(id);
  let kind = "";
  const rows: [string, ComponentChildren][] = [];
  let actions: ComponentChildren = null;

  const domain = data.domains.find((d) => d.id === id);
  const container = data.containers.find((c) => c.id === id);
  const storage = data.storage.find((s) => s.id === id);
  if (domain) {
    kind = "Domain";
    rows.push(["Forwards to", domain.forward]);
    if (domain.aliases?.length) rows.push(["Also", domain.aliases.join(", ")]);
    rows.push([
      "Certificate",
      domain.ssl
        ? domain.cert_days !== undefined
          ? `expires in ${domain.cert_days} days`
          : "yes"
        : "none",
    ]);
    if (domain.error) rows.push(["nginx", domain.error]);
    rows.push(["Reaches", domain.target ? name(domain.target) : "nothing — no such container"]);
    actions = (
      <a class="btn" href={`https://${domain.name}`} target="_blank" rel="noopener noreferrer">
        Open <ArrowUpRight size={14} />
      </a>
    );
  } else if (container) {
    kind = "Container";
    title = container.service?.name ?? container.name;
    rows.push(["Container", container.name]);
    rows.push(["Image", container.image]);
    rows.push(["Status", container.status]);
    if (container.project) rows.push(["Compose project", container.project]);
    if (container.published?.length)
      rows.push(["Ports", container.published.map((p) => `${p.host} → ${p.container}`).join(", ")]);
    if (container.networks?.length) rows.push(["Networks", container.networks.join(", ")]);
    if (incoming.length) rows.push(["Reached at", incoming.map((l) => name(l.from)).join(", ")]);
    if (outgoing.length)
      rows.push([
        "Mounts",
        <ul class="topo-mounts">
          {outgoing.map((l) => (
            <li key={l.to + l.label}>
              <code>{l.label}</code> ← {name(l.to)}
              {l.read_only && <span class="topo-badge">ro</span>}
            </li>
          ))}
        </ul>,
      ]);
    actions = (
      <>
        <button class="btn" onClick={() => props.onLogs(container.name)}>
          <ScrollText size={14} /> Logs
        </button>
        {container.service?.url && (
          <a class="btn" href={container.service.url} target="_blank" rel="noopener noreferrer">
            Open <ArrowUpRight size={14} />
          </a>
        )}
      </>
    );
  } else if (storage) {
    kind = storage.kind === "volume" ? "Volume" : "Folder";
    title = storage.kind === "volume" ? storage.name! : shortPath(storage.path);
    rows.push(["Host path", <code>{storage.path}</code>]);
    rows.push([
      "Used by",
      incoming
        .map((l) => `${name(l.from)} (${l.label}${l.read_only ? ", read-only" : ""})`)
        .join(", "),
    ]);
    rows.push([
      "Kopia",
      storage.backup
        ? `${storage.backup.partial ? "part of it, " : ""}snapshot ${storage.backup.source} · ${storage.backup.state}${
            storage.backup.last ? ` · ${timeAgo(new Date(storage.backup.last), now)}` : ""
          }`
        : "not in any snapshot",
    ]);
    if (storage.sync)
      rows.push([
        "Syncthing",
        `${storage.sync.partial ? "part of it, " : ""}folder “${storage.sync.folder}” · ${storage.sync.state}`,
      ]);
  } else if (id === "host" && data.host) {
    kind = "Host";
    rows.push(["Ports", data.host.ports.join(", ")]);
    rows.push(["Reached at", incoming.map((l) => name(l.from)).join(", ")]);
  }

  return (
    <div class="topo-inspector" ref={panel}>
      <div class="topo-inspector-head">
        <div>
          <p class="eyebrow eyebrow-accent">{kind}</p>
          <h2>{title}</h2>
        </div>
        <span class="spacer" />
        {actions}
        <button class="icon-btn" onClick={props.onClose} aria-label="Close">
          <X size={16} />
        </button>
      </div>
      <dl>
        {rows.map(([k, v]) => (
          <div key={k} class={k === "Mounts" ? "wide" : undefined}>
            <dt class="eyebrow">{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
