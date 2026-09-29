import { describe, expect, it } from "vitest";
import { parseRoute, routeHref } from "./router";

describe("router", () => {
  it("parses pages and the log viewer", () => {
    expect(parseRoute("")).toEqual({ page: "home", logs: null });
    expect(parseRoute("#/containers")).toEqual({ page: "containers", logs: null });
    expect(parseRoute("#/containers/?logs=uptime-kuma")).toEqual({
      page: "containers",
      logs: "uptime-kuma",
    });
    expect(parseRoute("#/?logs=a%20b")).toEqual({ page: "home", logs: "a b" });
    expect(parseRoute("#/drop?error=too+large")).toEqual({ page: "drop", logs: null });
    expect(parseRoute("#/nowhere")).toEqual({ page: "home", logs: null });
  });

  it("round-trips", () => {
    const route = { page: "containers" as const, logs: "romm-db" };
    expect(parseRoute(routeHref(route))).toEqual(route);
    expect(routeHref({ page: "drop", logs: null })).toBe("#/drop");
  });
});
