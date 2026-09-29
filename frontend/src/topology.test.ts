import { describe, expect, it } from "vitest";
import { chain, layout, protection, shortPath } from "./topology";
import type { Topology } from "./types";

const t: Topology = {
  domains: [
    {
      id: "d:b",
      name: "b.example.com",
      forward: "http://db:1",
      ssl: false,
      enabled: true,
      target: "c:db",
    },
    {
      id: "d:a",
      name: "a.example.com",
      forward: "http://web:80",
      ssl: true,
      enabled: true,
      target: "c:web",
    },
    { id: "d:x", name: "x.example.com", forward: "http://gone:80", ssl: false, enabled: true },
  ],
  containers: [
    { id: "c:web", name: "web", image: "nginx", state: "running", status: "Up", project: "main" },
    {
      id: "c:db",
      name: "db",
      image: "postgres",
      state: "exited",
      status: "Exited",
      project: "main",
    },
    { id: "c:solo", name: "solo", image: "alpine", state: "running", status: "Up" },
  ],
  storage: [
    { id: "p:/srv/web", kind: "bind", class: "data", path: "/srv/web", written: true },
    { id: "p:/etc/localtime", kind: "bind", class: "system", path: "/etc/localtime" },
    {
      id: "v:pg",
      kind: "volume",
      class: "data",
      path: "/var/lib/docker/volumes/pg/_data",
      name: "pg",
    },
  ],
  links: [
    { from: "d:a", to: "c:web" },
    { from: "d:b", to: "c:db" },
    { from: "c:web", to: "p:/srv/web", label: "/usr/share/nginx" },
    { from: "c:web", to: "p:/etc/localtime", label: "/etc/localtime", read_only: true },
    { from: "c:db", to: "v:pg", label: "/var/lib/postgresql" },
  ],
  issues: [],
  sources: {},
};

describe("topology", () => {
  it("follows a chain through a node", () => {
    expect([...chain(t, "d:a")].sort()).toEqual(["c:web", "d:a", "p:/etc/localtime", "p:/srv/web"]);
    expect([...chain(t, "v:pg")].sort()).toEqual(["c:db", "d:b", "v:pg"]);
  });

  it("orders domains by the container they reach and hides system mounts", () => {
    const v = layout(t, { query: "", system: false, stopped: true });
    expect(v.groups.map((g) => g.project)).toEqual(["main", "Standalone"]);
    expect(v.domains.map((d) => d.name)).toEqual([
      "b.example.com",
      "a.example.com",
      "x.example.com",
    ]);
    expect(v.storage.map((s) => s.id)).toEqual(["v:pg", "p:/srv/web"]);
  });

  it("drops stopped containers and the storage only they use", () => {
    const v = layout(t, { query: "", system: true, stopped: false });
    expect(v.groups.flatMap((g) => g.containers.map((c) => c.name))).toEqual(["web", "solo"]);
    expect(v.storage.map((s) => s.id)).not.toContain("v:pg");
  });

  it("keeps a search match's neighbours", () => {
    const v = layout(t, { query: "postgres", system: false, stopped: true });
    expect([...v.visible].sort()).toEqual(["c:db", "d:b", "v:pg"]);
  });

  it("describes protection", () => {
    expect(shortPath("/home/sam/stack/x")).toBe("~/stack/x");
    expect(protection(t.storage[0], true)).toEqual({ tone: "warn", label: "No backup" });
    expect(protection(t.storage[0], false)).toBeNull();
    expect(
      protection({ ...t.storage[0], backup: { state: "ok", source: "/data" } }, true)?.label,
    ).toBe("Backed up");
  });
});
