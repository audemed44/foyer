import { useEffect, useState } from "preact/hooks";

/**
 * Hash routes, so any URL works without server-side routing:
 *   #/                     dashboard
 *   #/containers           containers page
 *   …?logs=<name>          log viewer open over either page
 */
export type Route = { page: "home" | "containers"; logs: string | null };

export function parseRoute(hash: string): Route {
  const [path, query = ""] = hash.replace(/^#/, "").split("?");
  const page = path.replace(/\/+$/, "") === "/containers" ? "containers" : "home";
  const logs = new URLSearchParams(query).get("logs");
  return { page, logs: logs || null };
}

export function routeHref(route: Route): string {
  const path = route.page === "containers" ? "#/containers" : "#/";
  return route.logs ? `${path}?logs=${encodeURIComponent(route.logs)}` : path;
}

export function navigate(route: Route) {
  window.location.hash = routeHref(route);
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(window.location.hash));
  useEffect(() => {
    const onChange = () => setRoute(parseRoute(window.location.hash));
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return route;
}
