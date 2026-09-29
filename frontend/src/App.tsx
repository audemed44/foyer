import { Boxes, FolderPlus, LayoutGrid, Pencil, Settings2 } from "lucide-preact";
import { useCallback, useEffect, useState } from "preact/hooks";
import { api } from "./api";
import { Bookmarks } from "./components/Bookmarks";
import { ContainersPage } from "./components/Containers";
import { GroupDialog, ServiceDialog, SettingsDialog } from "./components/Editor";
import { Groups, type EditActions, type Position } from "./components/Groups";
import { Header } from "./components/Header";
import { LogViewer } from "./components/Logs";
import { Search } from "./components/Search";
import { Suggestions } from "./components/Suggestions";
import { usePoll } from "./hooks";
import { countStatus, newId } from "./lib";
import { navigate, routeHref, useRoute } from "./router";
import { applyTheme } from "./theme";
import type {
  Config,
  ConfigResponse,
  DockerContainer,
  Group,
  Service,
  StatusMap,
  Suggestion,
  Widget,
} from "./types";

type Modal =
  | { kind: "settings" }
  | { kind: "service"; group: number; index: number | null; prefill?: Service }
  | { kind: "group"; index: number | null };

export function App() {
  const [meta, setMeta] = useState<ConfigResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [draft, setDraft] = useState<Config | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [modal, setModal] = useState<Modal | null>(null);

  const reload = useCallback(async () => {
    try {
      setMeta(await api.config());
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  // The config file can be edited by hand; pick up changes every minute.
  useEffect(() => {
    reload();
    const t = window.setInterval(() => !draft && reload(), 60000);
    return () => window.clearInterval(t);
  }, [reload, draft]);

  const route = useRoute();
  const { data: status } = usePoll<StatusMap>(api.status, 15000);
  // Container stats for the service cards (memory on hover, logs button).
  const { data: containerList } = usePoll<DockerContainer[]>(api.containers, 15000);
  const containers = new Map((containerList ?? []).map((c) => [c.name, c]));

  const config = draft ?? meta?.config ?? null;
  useEffect(() => {
    if (config) applyTheme(config);
  }, [config]);

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  if (!config) {
    return <div class="boot">{loadError ? `Could not load the dashboard: ${loadError}` : ""}</div>;
  }

  const startEditing = async () => {
    try {
      setDraft(await api.editableConfig());
      setDirty(false);
      setSaveError(null);
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : String(e));
    }
  };

  const change = (next: Config) => {
    setDraft(next);
    setDirty(true);
  };

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setSaveError(null);
    try {
      await api.saveConfig(draft);
      setDraft(null);
      setDirty(false);
      await reload();
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const cancel = () => {
    if (dirty && !window.confirm("Discard your changes?")) return;
    setDraft(null);
    setDirty(false);
    setSaveError(null);
  };

  const mapGroups = (fn: (groups: Group[]) => Group[]) =>
    draft &&
    change({
      ...draft,
      groups: fn(draft.groups.map((g) => ({ ...g, services: [...g.services] }))),
    });

  const edit: EditActions | null = draft
    ? {
        editService: ({ group, index }) => setModal({ kind: "service", group, index }),
        addService: (group) => setModal({ kind: "service", group, index: null }),
        editGroup: (index) => setModal({ kind: "group", index }),
        moveService: (from: Position, to: Position) =>
          mapGroups((groups) => {
            const [moved] = groups[from.group].services.splice(from.index, 1);
            let index = to.index;
            if (from.group === to.group && from.index < to.index) index--;
            groups[to.group].services.splice(index, 0, moved);
            return groups;
          }),
        moveGroup: (index, delta) =>
          mapGroups((groups) => {
            const [g] = groups.splice(index, 1);
            groups.splice(index + delta, 0, g);
            return groups;
          }),
        deleteGroup: (index) => {
          const g = draft.groups[index];
          const msg = g.services.length
            ? `Delete “${g.name}” and its ${g.services.length} services?`
            : `Delete “${g.name}”?`;
          if (window.confirm(msg)) mapGroups((groups) => groups.filter((_, i) => i !== index));
        },
      }
    : null;

  const saveService = (service: Service, groupIndex: number) => {
    if (modal?.kind !== "service") return;
    mapGroups((groups) => {
      if (modal.index !== null) groups[modal.group].services.splice(modal.index, 1);
      const target = groups[groupIndex].services;
      const at = modal.index !== null && groupIndex === modal.group ? modal.index : target.length;
      target.splice(at, 0, service);
      return groups;
    });
    setModal(null);
  };

  // Discovery: open the service editor pre-filled from a container.
  const addSuggestion = (sg: Suggestion) => {
    if (!draft) return;
    let groups = draft.groups;
    if (groups.length === 0) {
      groups = [{ id: newId("group"), name: sg.group || "Apps", collapsed: false, services: [] }];
      change({ ...draft, groups });
    }
    const match = groups.findIndex((g) => g.name.toLowerCase() === (sg.group ?? "").toLowerCase());
    setModal({
      kind: "service",
      group: Math.max(0, match),
      index: null,
      prefill: {
        id: newId("service"),
        name: sg.name,
        url: sg.url,
        description: sg.description,
        icon: sg.icon,
        ping: sg.ping,
        container: sg.container,
        widget: sg.widget,
      },
    });
  };
  const addWidget = (serviceId: string, widget: Widget) =>
    mapGroups((groups) =>
      groups.map((g) => ({
        ...g,
        services: g.services.map((sv) => (sv.id === serviceId ? { ...sv, widget } : sv)),
      })),
    );
  const ignoreContainer = (name: string) =>
    draft && change({ ...draft, ignored_containers: [...draft.ignored_containers, name] });

  const openLogs = (name: string) => navigate({ ...route, logs: name });
  const closeLogs = () => navigate({ ...route, logs: null });

  const totals = countStatus(Object.values(status ?? {}));

  return (
    <div class={`page ${draft ? "is-editing" : ""}`}>
      {draft && (
        <div class="edit-bar" role="toolbar">
          <span class="edit-bar-label">
            <span class="dot warn" /> Editing{dirty ? " — unsaved changes" : ""}
          </span>
          {saveError && <span class="edit-bar-error">{saveError}</span>}
          <span class="spacer" />
          <button class="btn btn-ghost" onClick={() => setModal({ kind: "group", index: null })}>
            <FolderPlus size={14} /> Group
          </button>
          <button class="btn btn-ghost" onClick={() => setModal({ kind: "settings" })}>
            <Settings2 size={14} /> Settings
          </button>
          <button class="btn" onClick={cancel}>
            Cancel
          </button>
          <button class="btn btn-primary" onClick={save} disabled={saving || !dirty}>
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      )}

      {!draft && (
        <nav class="topnav">
          <a class="topnav-brand" href="#/">
            {config.title}
          </a>
          <span class="spacer" />
          <a
            class={`topnav-link ${route.page === "home" ? "on" : ""}`}
            href={routeHref({ page: "home", logs: null })}
          >
            <LayoutGrid size={14} /> Dashboard
          </a>
          <a
            class={`topnav-link ${route.page === "containers" ? "on" : ""}`}
            href={routeHref({ page: "containers", logs: null })}
          >
            <Boxes size={14} /> Containers
          </a>
          {route.page === "home" && (
            <button class="topnav-link" onClick={startEditing} title="Edit dashboard">
              <Pencil size={14} /> Edit
            </button>
          )}
        </nav>
      )}

      <main class="shell">
        {saveError && !draft && <div class="banner">{saveError}</div>}
        {meta?.error && (
          <div class="banner">
            Config error — showing the last good version. <code>{meta.error}</code>
          </div>
        )}
        {route.page === "containers" && !draft ? (
          <ContainersPage onLogs={openLogs} />
        ) : (
          <>
            <Header config={config} status={status ?? null} />
            <Search config={config} />
            {draft && (
              <Suggestions
                config={draft}
                onAdd={addSuggestion}
                onIgnore={ignoreContainer}
                onAddWidget={addWidget}
              />
            )}
            <Groups
              config={config}
              status={status ?? {}}
              containers={containers}
              onLogs={openLogs}
              edit={edit}
            />
            {config.groups.length === 0 && (
              <div class="empty">No services yet. Click Edit above to add some.</div>
            )}
            <Bookmarks config={config} />
          </>
        )}
        <footer class="foot">
          <span>
            {totals.total > 0 && (
              <>
                <span class={`dot ${totals.up < totals.total ? "bad" : "good"}`} /> {totals.up}/
                {totals.total} services up
              </>
            )}
          </span>
        </footer>
      </main>

      {route.logs && <LogViewer name={route.logs} onClose={closeLogs} />}

      {modal?.kind === "settings" && draft && (
        <SettingsDialog config={draft} onChange={change} onClose={() => setModal(null)} />
      )}
      {modal?.kind === "service" && draft && (
        <ServiceDialog
          service={
            modal.index !== null
              ? draft.groups[modal.group].services[modal.index]
              : (modal.prefill ?? { id: newId("service"), name: "" })
          }
          groups={draft.groups}
          groupIndex={modal.group}
          isNew={modal.index === null}
          widgetTypes={meta?.widget_types ?? []}
          onSave={saveService}
          onDelete={() => {
            mapGroups((groups) => {
              groups[modal.group].services.splice(modal.index!, 1);
              return groups;
            });
            setModal(null);
          }}
          onClose={() => setModal(null)}
        />
      )}
      {modal?.kind === "group" && draft && (
        <GroupDialog
          group={
            modal.index !== null
              ? draft.groups[modal.index]
              : { id: newId("group"), name: "", collapsed: false, services: [] }
          }
          isNew={modal.index === null}
          onSave={(g) => {
            mapGroups((groups) => {
              if (modal.index === null) groups.push(g);
              else groups[modal.index] = { ...g, services: groups[modal.index].services };
              return groups;
            });
            setModal(null);
          }}
          onClose={() => setModal(null)}
        />
      )}
    </div>
  );
}
