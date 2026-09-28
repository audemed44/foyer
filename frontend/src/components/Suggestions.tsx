import { LayoutPanelTop, Plus, Radar, X } from "lucide-preact";
import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import { linkedContainers } from "../lib";
import type { Config, DiscoverResponse, Suggestion, Widget } from "../types";
import { Icon } from "./Icon";

/**
 * Edit mode: running containers that aren't on the dashboard yet, ready to
 * add in one click (the service editor opens pre-filled).
 */
export function Suggestions(props: {
  config: Config;
  onAdd: (s: Suggestion) => void;
  onIgnore: (container: string) => void;
  onAddWidget: (serviceId: string, widget: Widget) => void;
}) {
  const [found, setFound] = useState<DiscoverResponse | null>(null);
  const [open, setOpen] = useState(true);
  useEffect(() => {
    api.discover().then(setFound, () => setFound({ containers: [], widgets: [] }));
  }, []);
  const items = found?.containers;

  // Apps on the dashboard that serve a Foyer widget their card doesn't show yet.
  const services = new Map(props.config.groups.flatMap((g) => g.services).map((s) => [s.id, s]));
  const widgetOffers = (found?.widgets ?? []).filter((w) => {
    const svc = services.get(w.service);
    return svc && !svc.widget;
  });

  // The draft may already include some of them; hide those as you add.
  const linked = linkedContainers(props.config);
  const visible = (items ?? []).filter(
    (s) => !linked.has(s.container) && !props.config.ignored_containers.includes(s.container),
  );
  if (visible.length === 0 && widgetOffers.length === 0) return null;

  return (
    <section class="suggest">
      {widgetOffers.map((w) => (
        <div class="suggest-head suggest-widget" key={w.service}>
          <LayoutPanelTop size={16} class="suggest-icon" />
          <p>
            <strong>{w.name} can show a live widget.</strong> It serves the Foyer widget format.
          </p>
          <span class="spacer" />
          <button
            class="btn btn-primary suggest-add"
            onClick={() => props.onAddWidget(w.service, w.widget)}
          >
            <Plus size={14} /> Add widget
          </button>
        </div>
      ))}
      {visible.length > 0 && (
        <div class="suggest-head">
          <Radar size={16} class="suggest-icon" />
          <p>
            <strong>
              {visible.length} running{" "}
              {visible.length === 1 ? "container isn’t" : "containers aren’t"} on your dashboard.
            </strong>{" "}
            Add them with one click, or dismiss the ones you don’t need.
          </p>
          <span class="spacer" />
          <button class="btn btn-ghost" onClick={() => setOpen(!open)}>
            {open ? "Hide" : "Show"}
          </button>
        </div>
      )}
      {open && visible.length > 0 && (
        <div class="suggest-list stagger">
          {visible.map((s) => (
            <div class="suggest-item" key={s.container}>
              <span class="tile-icon">
                <Icon icon={s.icon} name={s.name} size={24} />
              </span>
              <span class="suggest-text">
                <span class="tile-name">{s.name}</span>
                <span class="suggest-image">{s.image}</span>
              </span>
              <button class="btn btn-primary suggest-add" onClick={() => props.onAdd(s)}>
                <Plus size={14} /> Add
              </button>
              <button
                class="icon-btn"
                title="Don’t suggest this container again"
                aria-label={`Dismiss ${s.container}`}
                onClick={() => props.onIgnore(s.container)}
              >
                <X size={15} />
              </button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
