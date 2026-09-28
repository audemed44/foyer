import { Search as SearchIcon, CornerDownLeft } from "lucide-preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { searchServices, webSearchUrl } from "../lib";
import type { Config } from "../types";
import { Icon } from "./Icon";

/**
 * One box for both jobs: typing filters your services (Enter opens the
 * highlighted one), and when nothing matches Enter searches the web.
 * Press "/" anywhere to focus it.
 */
export function Search({ config }: { config: Config }) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const matches = useMemo(() => searchServices(config, query).slice(0, 6), [config, query]);
  const target = config.open_in_new_tab ? "_blank" : "_self";

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      const typing = el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName);
      if (e.key === "/" && !typing) {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => setActive(0), [query]);

  const open = (url: string) => {
    window.open(url, target, "noopener");
    setQuery("");
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, matches.length));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === "Escape") {
      setQuery("");
      input.current?.blur();
    } else if (e.key === "Enter" && query.trim()) {
      const service = matches[active];
      if (service?.url) open(service.url);
      else if (config.header.search.enabled) open(webSearchUrl(config, query));
    }
  };

  const webIndex = matches.length;
  const showWeb = config.header.search.enabled;

  return (
    <div class="search">
      <label class="search-box">
        <SearchIcon size={15} strokeWidth={1.75} />
        <input
          ref={input}
          value={query}
          onInput={(e) => setQuery(e.currentTarget.value)}
          onKeyDown={onKeyDown}
          placeholder={showWeb ? "Search services or the web" : "Find a service"}
          aria-label="Search"
          autocomplete="off"
          spellcheck={false}
        />
        <kbd>/</kbd>
      </label>
      {query.trim() && (matches.length > 0 || showWeb) && (
        <ul class="search-results" role="listbox">
          {matches.map((s, i) => (
            <li key={s.id} role="option" aria-selected={i === active}>
              <a
                href={s.url}
                target={target}
                rel="noopener noreferrer"
                class={i === active ? "active" : ""}
                onMouseEnter={() => setActive(i)}
                onClick={() => setQuery("")}
              >
                <Icon icon={s.icon} name={s.name} size={20} />
                <span class="search-name">{s.name}</span>
                <span class="search-desc">{s.description}</span>
                {i === active && <CornerDownLeft size={13} />}
              </a>
            </li>
          ))}
          {showWeb && (
            <li role="option" aria-selected={active === webIndex}>
              <a
                href={webSearchUrl(config, query)}
                target={target}
                rel="noopener noreferrer"
                class={`search-web ${active === webIndex ? "active" : ""}`}
                onMouseEnter={() => setActive(webIndex)}
                onClick={() => setQuery("")}
              >
                <SearchIcon size={15} />
                <span class="search-name">Search the web for “{query.trim()}”</span>
                {active === webIndex && <CornerDownLeft size={13} />}
              </a>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
