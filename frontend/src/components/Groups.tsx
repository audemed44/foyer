import {
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  ChevronDown,
  GripVertical,
  Pencil,
  Plus,
  ScrollText,
  Trash2,
} from "lucide-preact";
import { useState } from "preact/hooks";
import { useColumns } from "../hooks";
import { formatBytes, groupSpan, iconUrl, packGroups, serviceSpan } from "../lib";
import { useTint } from "../tint";
import type { Config, DockerContainer, Group, Service, ServiceStatus, StatusMap } from "../types";
import { WidgetBody } from "../widgets";
import { Icon } from "./Icon";

export type Position = { group: number; index: number };

export type EditActions = {
  editService: (pos: Position) => void;
  addService: (group: number) => void;
  moveService: (from: Position, to: Position) => void;
  editGroup: (group: number) => void;
  moveGroup: (group: number, delta: number) => void;
  deleteGroup: (group: number) => void;
};

type Props = {
  config: Config;
  status: StatusMap;
  containers: Map<string, DockerContainer>;
  onLogs: (container: string) => void;
  edit: EditActions | null;
};

export function Groups({ config, status, containers, onLogs, edit }: Props) {
  const { ref, columns } = useColumns(config.theme.columns, 260);
  // In edit mode the "Add service" tile needs a cell too.
  const spanOf = (g: Group) => Math.min(columns, groupSpan(g, columns) + (edit ? 1 : 0));
  const order = packGroups(config.groups.map(spanOf), columns);
  return (
    <div ref={ref} class="groups" style={{ "--cols": columns }}>
      {order.map((gi, position) => (
        <GroupSection
          key={config.groups[gi].id}
          group={config.groups[gi]}
          index={gi}
          number={position + 1}
          span={spanOf(config.groups[gi])}
          count={config.groups.length}
          columns={columns}
          config={config}
          status={status}
          containers={containers}
          onLogs={onLogs}
          edit={edit}
        />
      ))}
    </div>
  );
}

const collapsedKey = (id: string) => `foyer:collapsed:${id}`;

function readCollapsed(group: Group): boolean {
  try {
    const v = localStorage.getItem(collapsedKey(group.id));
    return v === null ? group.collapsed : v === "1";
  } catch {
    return group.collapsed;
  }
}

function GroupSection(props: {
  group: Group;
  index: number;
  number: number;
  span: number;
  count: number;
  columns: number;
  config: Config;
  status: StatusMap;
  containers: Map<string, DockerContainer>;
  onLogs: (container: string) => void;
  edit: EditActions | null;
}) {
  const { group, index, columns, config, status, edit, span } = props;
  const [collapsed, setCollapsed] = useState(() => readCollapsed(group));
  const [dropping, setDropping] = useState(false);
  // On a phone the page is one column; lay tiles out two-up instead of a long list.
  const tileCols = columns === 1 ? 2 : span;
  const pinged = group.services.map((s) => status[s.id]?.ping).filter(Boolean);
  const down = pinged.filter((p) => p!.state === "down").length;

  const toggle = () => {
    const next = !collapsed;
    setCollapsed(next);
    try {
      localStorage.setItem(collapsedKey(group.id), next ? "1" : "0");
    } catch {
      // storage unavailable; the toggle still works for this visit
    }
  };

  const isCollapsed = collapsed && !edit;
  // One-column groups keep the status short so the heading has room.
  const narrow = span === 1 && columns > 1;

  return (
    <section
      class={`group ${dropping ? "dropping" : ""}`}
      style={{ "--span": span }}
      onDragOver={(e) => {
        if (!edit) return;
        e.preventDefault();
        setDropping(true);
      }}
      onDragLeave={() => setDropping(false)}
      onDrop={(e) => {
        setDropping(false);
        if (!edit) return;
        const from = readDrag(e);
        if (from) edit.moveService(from, { group: index, index: group.services.length });
      }}
    >
      <div class="group-head">
        <button class="group-title" onClick={toggle} aria-expanded={!isCollapsed} disabled={!!edit}>
          <span class="group-index">{String(props.number).padStart(2, "0")}</span>
          <h2 class="group-name">{group.name}</h2>
          {!edit && (
            <ChevronDown size={16} class={`group-chevron ${isCollapsed ? "closed" : ""}`} />
          )}
        </button>
        <span class="spacer" />
        {edit ? (
          <span class="group-tools">
            <button
              title="Move up"
              onClick={() => edit.moveGroup(index, -1)}
              disabled={index === 0}
            >
              <ArrowUp size={14} />
            </button>
            <button
              title="Move down"
              onClick={() => edit.moveGroup(index, 1)}
              disabled={index === props.count - 1}
            >
              <ArrowDown size={14} />
            </button>
            <button title="Rename" onClick={() => edit.editGroup(index)}>
              <Pencil size={14} />
            </button>
            <button title="Delete group" onClick={() => edit.deleteGroup(index)}>
              <Trash2 size={14} />
            </button>
          </span>
        ) : (
          <span class={`eyebrow group-meta ${down ? "bad" : ""}`}>
            {pinged.length > 0 && <span class={`dot ${down ? "bad" : "good"}`} />}
            {pinged.length === 0
              ? `${group.services.length} ${group.services.length === 1 ? "app" : "apps"}`
              : down
                ? `${down} down`
                : narrow
                  ? `${pinged.length}/${pinged.length}`
                  : `${pinged.length} of ${pinged.length} up`}
          </span>
        )}
      </div>
      {!isCollapsed && (
        <div class="tiles stagger" style={{ "--tile-cols": tileCols }}>
          {group.services.map((service, si) => (
            <ServiceCard
              key={service.id}
              service={service}
              status={status[service.id]}
              container={props.containers.get(status[service.id]?.container?.name ?? "")}
              onLogs={props.onLogs}
              span={serviceSpan(service, tileCols)}
              newTab={config.open_in_new_tab}
              position={{ group: index, index: si }}
              edit={edit}
            />
          ))}
          {edit && (
            <button
              class="tile tile-add"
              style={{ "--span": 1 }}
              onClick={() => edit.addService(index)}
            >
              <Plus size={18} />
              <span>Add service</span>
            </button>
          )}
        </div>
      )}
    </section>
  );
}

const DRAG_TYPE = "application/x-foyer-service";

function readDrag(e: DragEvent): Position | null {
  const raw = e.dataTransfer?.getData(DRAG_TYPE);
  if (!raw) return null;
  e.stopPropagation();
  return JSON.parse(raw);
}

function statusInfo(st?: ServiceStatus): { tone: string; label: string } | null {
  const c = st?.container;
  const p = st?.ping;
  if (p?.state === "down") return { tone: "bad", label: p.error ?? `HTTP ${p.code}` };
  if (c && c.state !== "running") return { tone: "bad", label: c.state };
  if (c?.health === "unhealthy") return { tone: "warn", label: "unhealthy" };
  if (c?.health === "starting") return { tone: "warn", label: "starting" };
  if (p?.state === "up") return { tone: "good", label: `${p.latency_ms ?? 0}ms` };
  if (c) return { tone: "good", label: c.health ?? "running" };
  return null;
}

function ServiceCard(props: {
  service: Service;
  status?: ServiceStatus;
  container?: DockerContainer;
  onLogs: (container: string) => void;
  span: number;
  newTab: boolean;
  position: Position;
  edit: EditActions | null;
}) {
  const { service, span, edit, position } = props;
  const [over, setOver] = useState(false);
  const info = statusInfo(props.status);
  // A problem replaces the description, so it's readable even on small tiles.
  const problem = info && info.tone !== "good";
  const tooltip = [
    service.url,
    info && `status: ${info.label}`,
    props.status?.container?.status && `container: ${props.status.container.status}`,
  ]
    .filter(Boolean)
    .join("\n");

  const tint = useTint(iconUrl(service.icon));
  const latency = info?.tone === "good" && props.status?.ping ? info.label : null;
  const container = props.container;
  const memory = container?.stats ? formatBytes(container.stats.mem_used) : null;
  const hoverInfo = [latency, memory].filter(Boolean).join(" · ");

  const head = (
    <>
      <span class="tile-icon">
        <Icon icon={service.icon} name={service.name} size={service.widget ? 24 : 28} />
      </span>
      <span class="tile-text">
        <span class="tile-name">{service.name}</span>
        {problem ? (
          <span class={`tile-desc tile-problem ${info!.tone}`}>{info!.label}</span>
        ) : (
          service.description && <span class="tile-desc">{service.description}</span>
        )}
      </span>
      <span class="tile-side">
        {hoverInfo && <span class="tile-latency">{hoverInfo}</span>}
        {info && <span class={`dot ${info.tone}`} />}
      </span>
      {!edit && service.url && <ArrowUpRight size={15} class="tile-arrow" />}
    </>
  );

  const dragProps = edit
    ? {
        draggable: true,
        onDragStart: (e: DragEvent) => {
          e.dataTransfer!.setData(DRAG_TYPE, JSON.stringify(position));
          e.dataTransfer!.effectAllowed = "move";
        },
        onDragOver: (e: DragEvent) => {
          e.preventDefault();
          e.stopPropagation();
          setOver(true);
        },
        onDragLeave: () => setOver(false),
        onDrop: (e: DragEvent) => {
          setOver(false);
          const from = readDrag(e);
          if (from) edit.moveService(from, position);
        },
      }
    : {};

  const cls = [
    "tile",
    service.widget && "tile-widget",
    edit && "editing",
    over && "drop-before",
    problem && `is-${info!.tone}`,
    tint && "tinted",
  ]
    .filter(Boolean)
    .join(" ");
  const style = { "--span": span, ...(tint ? { "--tint": tint } : {}) };

  if (edit) {
    return (
      <div class={cls} style={style} {...dragProps}>
        <button class="tile-head tile-edit" onClick={() => edit.editService(position)}>
          <GripVertical size={14} class="grip" />
          {head}
          <Pencil size={13} class="tile-pencil" />
        </button>
        {service.widget && <div class="widget-placeholder">{service.widget.type} widget</div>}
      </div>
    );
  }

  const target = props.newTab ? "_blank" : undefined;
  return (
    <div class={`${cls} ${service.widget ? "" : "is-link"}`} style={style}>
      <a
        class="tile-head"
        href={service.url}
        target={target}
        rel="noopener noreferrer"
        title={tooltip}
      >
        {head}
      </a>
      {container && (
        <button
          class="tile-logs"
          title={`Logs for ${container.name}`}
          aria-label={`Logs for ${container.name}`}
          onClick={() => props.onLogs(container.name)}
        >
          <ScrollText size={14} />
        </button>
      )}
      {service.widget && <WidgetBody service={service} />}
    </div>
  );
}
