import { useEffect, useRef, useState } from "preact/hooks";

/** Calls `load` now and every `ms` while the tab is visible. */
export function usePoll<T>(load: () => Promise<T>, ms: number, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    const tick = async () => {
      window.clearTimeout(timer);
      if (document.visibilityState === "visible") {
        try {
          const result = await loadRef.current();
          if (!cancelled) {
            setData(result);
            setError(null);
          }
        } catch (e) {
          if (!cancelled) setError(e instanceof Error ? e.message : String(e));
        }
      }
      if (!cancelled) timer = window.setTimeout(tick, ms);
    };
    const onVisible = () => document.visibilityState === "visible" && tick();
    tick();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ms, ...deps]);

  return { data, error };
}

/** The current time, updated on each minute (or second) boundary. */
export function useNow(everySecond = false): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    let timer: number;
    const schedule = () => {
      const d = new Date();
      const wait = everySecond ? 1000 - d.getMilliseconds() : 60000 - (d.getTime() % 60000);
      timer = window.setTimeout(() => {
        setNow(new Date());
        schedule();
      }, wait + 5);
    };
    schedule();
    return () => window.clearTimeout(timer);
  }, [everySecond]);
  return now;
}

/** Number of grid columns that fit, capped at `max`. */
export function useColumns(max: number, minWidth = 250) {
  const ref = useRef<HTMLDivElement>(null);
  const [columns, setColumns] = useState(max);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      const fit = Math.max(1, Math.floor((entry.contentRect.width + 16) / minWidth));
      setColumns(Math.min(max, fit));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [max, minWidth]);
  return { ref, columns };
}

/** Whether a media query matches, kept up to date. */
export function useMedia(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const onChange = () => setMatches(mq.matches);
    onChange();
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [query]);
  return matches;
}
