import type { Config } from "../types";
import { Icon } from "./Icon";

export function Bookmarks({ config }: { config: Config }) {
  const groups = config.bookmarks.filter((g) => g.links.length > 0);
  if (groups.length === 0) return null;
  const target = config.open_in_new_tab ? "_blank" : undefined;
  return (
    <section class="bookmarks">
      {groups.map((g) => (
        <div class="bookmark-group" key={g.name}>
          <div class="bookmark-label">{g.name}</div>
          <div class="bookmark-links">
            {g.links.map((l) => (
              <a key={l.url + l.name} href={l.url} target={target} rel="noopener noreferrer">
                {l.icon ? (
                  <Icon icon={l.icon} name={l.name} size={14} />
                ) : (
                  <span class="abbr">{(l.abbr || l.name.slice(0, 2)).toUpperCase()}</span>
                )}
                {l.name}
              </a>
            ))}
          </div>
        </div>
      ))}
    </section>
  );
}
