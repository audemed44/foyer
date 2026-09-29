import type { TopoContainer, TopoDomain, TopoStorage, Topology } from "./types";

// Pure helpers for the topology page: ordering, filtering and the chains
// highlighted on hover. The page itself only lays out and draws.

export type Adjacency = Map<string, Set<string>>;

function adjacency(t: Topology): { out: Adjacency; in: Adjacency } {
  const out: Adjacency = new Map();
  const inn: Adjacency = new Map();
  const add = (m: Adjacency, a: string, b: string) => {
    if (!m.has(a)) m.set(a, new Set());
    m.get(a)!.add(b);
  };
  for (const l of t.links) {
    add(out, l.from, l.to);
    add(inn, l.to, l.from);
  }
  return { out, in: inn };
}

/**
 * Everything on the path through a node: what reaches it (a domain for a
 * container, containers for storage) and what it reaches, transitively.
 */
export function chain(t: Topology, id: string): Set<string> {
  const { out, in: inn } = adjacency(t);
  const seen = new Set([id]);
  const walk = (m: Adjacency, from: string) => {
    for (const next of m.get(from) ?? []) {
      if (seen.has(next)) continue;
      seen.add(next);
      walk(m, next);
    }
  };
  walk(out, id);
  walk(inn, id);
  return seen;
}

export type Filters = { query: string; system: boolean; stopped: boolean };

export type Layout = {
  domains: TopoDomain[];
  groups: { project: string; containers: TopoContainer[] }[];
  storage: TopoStorage[];
  visible: Set<string>;
};

const STANDALONE = "Standalone";

/**
 * Orders the three columns so links cross as little as possible: containers
 * by compose project, domains by the container they reach, storage by the
 * first container that mounts it. Filters hide nodes (and keep the
 * neighbours of anything a search matches, so matches stay in context).
 */
export function layout(t: Topology, f: Filters): Layout {
  const q = f.query.trim().toLowerCase();
  const storageById = new Map(t.storage.map((s) => [s.id, s]));

  let containers = t.containers.filter((c) => f.stopped || c.state === "running");
  const shown = new Set(containers.map((c) => c.id));
  shown.add("host");
  for (const d of t.domains) shown.add(d.id);
  for (const s of t.storage) if (f.system || s.class === "data") shown.add(s.id);
  // Storage only used by hidden containers goes too.
  const used = new Set(
    t.links.filter((l) => shown.has(l.from) && l.from.startsWith("c:")).map((l) => l.to),
  );
  for (const s of t.storage) if (!used.has(s.id)) shown.delete(s.id);

  let visible = shown;
  if (q) {
    const matches = new Set<string>();
    const hit = (id: string, ...texts: (string | undefined)[]) => {
      if (shown.has(id) && texts.some((x) => x?.toLowerCase().includes(q))) matches.add(id);
    };
    for (const d of t.domains) hit(d.id, d.name, d.forward, d.service?.name, ...(d.aliases ?? []));
    for (const c of t.containers) hit(c.id, c.name, c.image, c.project, c.service?.name);
    for (const s of t.storage) hit(s.id, s.path, s.name, s.sync?.folder);
    visible = new Set(matches);
    for (const l of t.links) {
      if (matches.has(l.from) && shown.has(l.to)) visible.add(l.to);
      if (matches.has(l.to) && shown.has(l.from)) visible.add(l.from);
    }
  }
  containers = containers.filter((c) => visible.has(c.id));

  const byProject = new Map<string, TopoContainer[]>();
  for (const c of containers) {
    const key = c.project || STANDALONE;
    if (!byProject.has(key)) byProject.set(key, []);
    byProject.get(key)!.push(c);
  }
  const groups = [...byProject.entries()]
    .sort(
      ([a, ca], [b, cb]) =>
        Number(a === STANDALONE) - Number(b === STANDALONE) ||
        cb.length - ca.length ||
        a.localeCompare(b),
    )
    .map(([project, list]) => ({
      project,
      containers: list.sort((a, b) => a.name.localeCompare(b.name)),
    }));

  const rank = new Map<string, number>();
  rank.set("host", -1);
  groups.flatMap((g) => g.containers).forEach((c, i) => rank.set(c.id, i));
  const rankOf = (id?: string) => (id && rank.has(id) ? rank.get(id)! : 1e6);

  const domains = t.domains
    .filter((d) => visible.has(d.id))
    .sort((a, b) => rankOf(a.target) - rankOf(b.target) || a.name.localeCompare(b.name));

  const firstUser = new Map<string, number>();
  for (const l of t.links) {
    if (!l.from.startsWith("c:") || !visible.has(l.from)) continue;
    firstUser.set(l.to, Math.min(firstUser.get(l.to) ?? 1e6, rankOf(l.from)));
  }
  const storage = [...visible]
    .map((id) => storageById.get(id))
    .filter((s): s is TopoStorage => !!s)
    .sort(
      (a, b) =>
        (firstUser.get(a.id) ?? 1e6) - (firstUser.get(b.id) ?? 1e6) || a.path.localeCompare(b.path),
    );

  return { domains, groups, storage, visible };
}

/** Shortens host paths for display: /home/sam/x → ~/x. */
export function shortPath(p: string): string {
  return p.replace(/^\/home\/[^/]+(?=\/|$)/, "~");
}

/** How a storage node's protection reads, for its badge. */
export function protection(s: TopoStorage, kopia: boolean): { tone: string; label: string } | null {
  if (s.backup && !s.backup.partial) {
    if (s.backup.state === "ok" || s.backup.state === "running")
      return { tone: "good", label: "Backed up" };
    if (s.backup.state === "errors") return { tone: "warn", label: "Backup has errors" };
    return { tone: "bad", label: `Backup ${s.backup.state}` };
  }
  if (!kopia || s.class !== "data" || !s.written) return null;
  if (s.backup?.partial) return { tone: "warn", label: "Partly backed up" };
  return { tone: "warn", label: "No backup" };
}
