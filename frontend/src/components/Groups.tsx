import { ArrowDown, ArrowUp, ChevronDown, GripVertical, Pencil, Plus, Trash2 } from "lucide-preact";
import { useState } from "preact/hooks";
import { useColumns } from "../hooks";
import { groupSpan, packGroups, serviceSpan } from "../lib";
import type { Config, Group, Service, ServiceStatus, StatusMap } from "../types";
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
  edit: EditActions | null;
};

export function Groups({ config, status, edit }: Props) {
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
          <span class="group-name">{group.name}</span>
          {!edit && (
            <ChevronDown size={13} class={`group-chevron ${isCollapsed ? "closed" : ""}`} />
          )}
        </button>
        <span class="group-rule" />
        {edit ? (
          <span class="group-tools">
            <button
              title="Move up"
              onClick={() => edit.moveGroup(index, -1)}
              disabled={index === 0}
            >
              <ArrowUp size={13} />
            </button>
            <button
              title="Move down"
              onClick={() => edit.moveGroup(index, 1)}
              disabled={index === props.count - 1}
            >
              <ArrowDown size={13} />
            </button>
            <button title="Rename" onClick={() => edit.editGroup(index)}>
              <Pencil size={13} />
            </button>
            <button title="Delete group" onClick={() => edit.deleteGroup(index)}>
              <Trash2 size={13} />
            </button>
          </span>
        ) : (
          <span class={`group-meta ${down ? "bad" : ""}`}>
            {pinged.length > 0
              ? down
                ? `${down} down`
                : `${pinged.length}/${pinged.length} up`
              : `${group.services.length}`}
          </span>
        )}
      </div>
      {!isCollapsed && (
        <div class="tiles" style={{ "--tile-cols": tileCols }}>
          {group.services.map((service, si) => (
            <ServiceCard
              key={service.id}
              service={service}
              status={status[service.id]}
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
              <Plus size={16} />
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

  const head = (
    <>
      <Icon icon={service.icon} name={service.name} size={service.widget ? 26 : 32} />
      <div class="tile-text">
        <div class="tile-name">{service.name}</div>
        {problem ? (
          <div class={`tile-desc tile-problem ${info!.tone}`}>{info!.label}</div>
        ) : (
          service.description && <div class="tile-desc">{service.description}</div>
        )}
      </div>
      {info && <span class={`dot tile-dot ${info.tone}`} />}
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

  const cls = `tile ${service.widget ? "tile-widget" : ""} ${edit ? "editing" : ""} ${over ? "drop-before" : ""}`;
  const style = { "--span": span };

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
  if (!service.widget) {
    return (
      <a
        class={cls}
        style={style}
        href={service.url}
        target={target}
        rel="noopener noreferrer"
        title={tooltip}
      >
        <div class="tile-head">{head}</div>
      </a>
    );
  }
  return (
    <div class={cls} style={style}>
      <a
        class="tile-head"
        href={service.url}
        target={target}
        rel="noopener noreferrer"
        title={tooltip}
      >
        {head}
      </a>
      <WidgetBody service={service} />
    </div>
  );
}
