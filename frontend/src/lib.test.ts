import { describe, expect, it } from "vitest";
import {
  groupSpan,
  iconUrl,
  matchScore,
  monogram,
  packGroups,
  searchServices,
  webSearchUrl,
} from "./lib";
import type { Config, Group } from "./types";

describe("iconUrl", () => {
  it("resolves dashboard-icons names by extension", () => {
    expect(iconUrl("sonarr.png")).toMatch(/\/png\/sonarr\.png$/);
    expect(iconUrl("jellyfin.svg")).toMatch(/\/svg\/jellyfin\.svg$/);
    expect(iconUrl("plex")).toMatch(/\/png\/plex\.png$/);
  });
  it("passes URLs and local paths through", () => {
    expect(iconUrl("/icons/app.png")).toBe("/icons/app.png");
    expect(iconUrl("https://x.dev/a.png")).toBe("https://x.dev/a.png");
  });
  it("maps simple-icons", () => {
    expect(iconUrl("si-github")).toMatch(/simple-icons.*\/github\.svg$/);
  });
  it("returns null for empty values", () => {
    expect(iconUrl("")).toBeNull();
    expect(iconUrl(undefined)).toBeNull();
  });
});

describe("monogram", () => {
  it("uses initials, camel caps or the first two letters", () => {
    expect(monogram("Code Server")).toBe("CS");
    expect(monogram("qBittorrent")).toBe("QB");
    expect(monogram("PocketLog")).toBe("PL");
    expect(monogram("it-tools")).toBe("IT");
  });
});

const svc = (name: string, description = "") => ({ id: name.toLowerCase(), name, description });

describe("search", () => {
  it("ranks prefix matches above substring and fuzzy ones", () => {
    expect(matchScore(svc("Komodo"), "kom")).toBeGreaterThan(matchScore(svc("Uptime-Kuma"), "kum"));
    expect(matchScore(svc("qBittorrent"), "qbt")).toBeGreaterThan(0);
    expect(matchScore(svc("Dozzle"), "xyz")).toBe(0);
  });

  it("orders results by score", () => {
    const config = {
      groups: [{ services: [svc("Cloudbeaver", "db"), svc("Code Server"), svc("Cockpit")] }],
    } as unknown as Config;
    expect(searchServices(config, "co").map((s) => s.name)).toEqual([
      "Code Server",
      "Cockpit",
      "Cloudbeaver",
    ]);
  });

  it("builds web search URLs", () => {
    const config = {
      header: { search: { provider: "custom", url: "https://s.lan/?q=" } },
    } as Config;
    expect(webSearchUrl(config, "a b")).toBe("https://s.lan/?q=a%20b");
  });
});

describe("layout", () => {
  const group = (cards: number, widgets = 0): Group => ({
    id: "g",
    name: "g",
    collapsed: false,
    services: [
      ...Array.from({ length: cards }, (_, i) => ({ id: `s${i}`, name: "s" })),
      ...Array.from({ length: widgets }, (_, i) => ({
        id: `w${i}`,
        name: "w",
        widget: { type: "x" },
      })),
    ],
  });

  it("sizes groups to their cards, capped at the page width", () => {
    expect(groupSpan(group(1), 4)).toBe(1);
    expect(groupSpan(group(0, 1), 4)).toBe(2);
    expect(groupSpan(group(9), 4)).toBe(4);
  });

  it("packs small groups into gaps, keeping order within rows", () => {
    // spans: 2, 4, 1, 1, 1, 2, 2  on 4 columns
    expect(packGroups([2, 4, 1, 1, 1, 2, 2], 4)).toEqual([0, 2, 3, 1, 4, 5, 6]);
    expect(packGroups([1, 1, 1], 1)).toEqual([0, 1, 2]);
  });
});
