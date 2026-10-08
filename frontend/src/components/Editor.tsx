import { Plus, Trash2 } from "lucide-preact";
import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import { timeAgo } from "../lib";
import type { Alerts, AlertsResponse, Config, Group, Service, Theme, Widget } from "../types";
import { Icon } from "./Icon";
import { Dialog, Field, Segmented, Select, TextInput, Toggle } from "./ui";

export const SECRET_MASK = "__foyer_secret__";

// ── Service ──────────────────────────────────────────────────────────────

type WidgetField = {
  key: string;
  label: string;
  hint?: string;
  secret?: boolean;
  number?: boolean;
};

const WIDGET_FIELDS: Record<string, WidgetField[]> = {
  app: [
    {
      key: "url",
      label: "Widget URL",
      hint: "An app serving the Foyer widget format, e.g. http://shelfloom:8000/api/foyer/widget",
    },
    { key: "key", label: "API token (optional)", secret: true, hint: "Sent as a bearer token." },
  ],
  uptimekuma: [
    { key: "url", label: "Uptime Kuma URL", hint: "As reachable from the Foyer container." },
    { key: "slug", label: "Status page slug" },
  ],
  speedtest: [
    { key: "url", label: "Speedtest Tracker URL" },
    {
      key: "key",
      label: "API token",
      secret: true,
      hint: "Or ${ENV_VAR} to read it from the environment.",
    },
    {
      key: "version",
      label: "API version",
      number: true,
      hint: "2 for Speedtest Tracker 1.x+, 1 for older.",
    },
  ],
  calendar: [
    { key: "url", label: "iCal URL" },
    { key: "days", label: "Days ahead", number: true },
    { key: "max_events", label: "Max events", number: true },
  ],
  kopia: [
    {
      key: "url",
      label: "Kopia server URL",
      hint: "The address of `kopia server start`, e.g. http://host.docker.internal:51515",
    },
    { key: "username", label: "Server username", hint: "--server-username" },
    { key: "password", label: "Server password", secret: true },
    {
      key: "stale_hours",
      label: "Warn after (hours)",
      number: true,
      hint: "A source with no snapshot for this long is flagged. Default 48.",
    },
  ],
  syncthing: [
    { key: "url", label: "Syncthing URL", hint: "The GUI address, e.g. http://syncthing:8384" },
    { key: "key", label: "API key", secret: true, hint: "Settings → General → API Key." },
  ],
  lookout: [
    { key: "url", label: "Lookout URL", hint: "e.g. http://lookout:8080" },
    {
      key: "key",
      label: "Token",
      secret: true,
      hint: "LOOKOUT_TOKEN, or ${ENV_VAR}. Lookout's checks then become the services' status.",
    },
  ],
  keep: [
    { key: "url", label: "Keep URL", hint: "e.g. http://keep:8080" },
    {
      key: "key",
      label: "Token",
      secret: true,
      hint: "KEEP_TOKEN, or ${ENV_VAR}. Keep's sources then show on the map and in backup alerts.",
    },
  ],
  gatehouse: [
    {
      key: "url",
      label: "Gatehouse admin URL",
      hint: "The admin port, e.g. http://gatehouse:8081",
    },
    {
      key: "key",
      label: "Token",
      secret: true,
      hint: "GATEHOUSE_TOKEN, or ${ENV_VAR} to read it from the environment.",
    },
    {
      key: "warn_days",
      label: "Warn before expiry (days)",
      number: true,
      hint: "Default: Gatehouse's own setting.",
    },
  ],
  npm: [
    { key: "url", label: "NPM admin URL", hint: "The admin port, e.g. http://npm:81" },
    { key: "email", label: "Email" },
    { key: "password", label: "Password", secret: true },
    {
      key: "warn_days",
      label: "Warn before expiry (days)",
      number: true,
      hint: "Default 14.",
    },
  ],
  komodo: [
    { key: "url", label: "Komodo URL", hint: "e.g. http://host.docker.internal:9120" },
    {
      key: "key",
      label: "API key",
      secret: true,
      hint: "From a service user with read access (see the README).",
    },
    { key: "secret", label: "API secret", secret: true },
  ],
};

const WIDGET_LABELS: Record<string, string> = {
  app: "App (Foyer widget)",
  uptimekuma: "Uptime Kuma",
  speedtest: "Speedtest Tracker",
  calendar: "Calendar (iCal)",
  kopia: "Kopia backups",
  syncthing: "Syncthing",
  npm: "Nginx Proxy Manager",
  gatehouse: "Gatehouse",
  lookout: "Lookout",
  keep: "Keep backups",
  komodo: "Komodo",
};

export function ServiceDialog(props: {
  service: Service;
  groups: Group[];
  groupIndex: number;
  isNew: boolean;
  widgetTypes: string[];
  onSave: (service: Service, groupIndex: number) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const [s, setS] = useState<Service>(props.service);
  const [group, setGroup] = useState(props.groupIndex);
  const [icons, setIcons] = useState<string[]>([]);
  useEffect(() => {
    api.icons().then(setIcons, () => {});
  }, []);

  const set = <K extends keyof Service>(key: K, value: Service[K]) =>
    setS((prev) => ({ ...prev, [key]: value }));
  const setWidget = (key: string, value: unknown) =>
    setS((prev) => {
      const widget: Widget = { ...(prev.widget as Widget), [key]: value };
      if (value === "" || value === undefined) delete widget[key];
      return { ...prev, widget };
    });
  const widgetType = s.widget?.type ?? "";

  return (
    <Dialog
      title={props.isNew ? "Add service" : `Edit ${props.service.name}`}
      onClose={props.onClose}
      footer={
        <>
          {!props.isNew && (
            <button class="btn btn-danger" onClick={props.onDelete}>
              <Trash2 size={14} /> Delete
            </button>
          )}
          <span class="spacer" />
          <button class="btn" onClick={props.onClose}>
            Cancel
          </button>
          <button
            class="btn btn-primary"
            disabled={!s.name.trim()}
            onClick={() => props.onSave(s, group)}
          >
            {props.isNew ? "Add" : "Done"}
          </button>
        </>
      }
    >
      <div class="icon-preview">
        <Icon icon={s.icon} name={s.name || "?"} size={44} />
        <div class="grow">
          <Field label="Name">
            <TextInput value={s.name} onChange={(v) => set("name", v)} autofocus={props.isNew} />
          </Field>
        </div>
      </div>
      <Field label="Link">
        <TextInput value={s.url} onChange={(v) => set("url", v)} placeholder="https://" />
      </Field>
      <Field label="Description">
        <TextInput value={s.description} onChange={(v) => set("description", v)} />
      </Field>
      <Field
        label="Icon"
        hint="A dashboard-icons name (sonarr.png, jellyfin.svg), si-github, a URL, or a file in /config/icons."
      >
        <TextInput value={s.icon} onChange={(v) => set("icon", v)} list="foyer-icons" />
        <datalist id="foyer-icons">
          {icons.map((i) => (
            <option key={i} value={i} />
          ))}
        </datalist>
      </Field>
      <div class="row">
        <Field
          label="Status check URL"
          hint="Optional. Without one, the linked container's state is the status"
        >
          <TextInput value={s.ping} onChange={(v) => set("ping", v)} placeholder="optional" />
        </Field>
        <Field label="Docker container" hint="Shows running / health state.">
          <TextInput
            value={s.container}
            onChange={(v) => set("container", v)}
            placeholder="optional"
          />
        </Field>
      </div>
      <Field label="Group">
        <Select
          value={String(group)}
          options={props.groups.map((g, i) => [String(i), g.name] as [string, string])}
          onChange={(v) => setGroup(Number(v))}
        />
      </Field>

      <div class="subhead">Widget</div>
      <Field label="Type">
        <Select
          value={widgetType}
          options={[
            ["", "None"],
            ...props.widgetTypes.map((t) => [t, WIDGET_LABELS[t] ?? t] as [string, string]),
          ]}
          onChange={(v) => set("widget", v ? { type: v } : undefined)}
        />
      </Field>
      {s.widget &&
        (WIDGET_FIELDS[widgetType] ?? []).map((f) => (
          <Field key={f.key} label={f.label} hint={f.hint}>
            {f.secret ? (
              <TextInput
                type="password"
                value={s.widget![f.key] === SECRET_MASK ? "" : String(s.widget![f.key] ?? "")}
                placeholder={s.widget![f.key] === SECRET_MASK ? "Saved — type to replace" : ""}
                onChange={(v) =>
                  setWidget(
                    f.key,
                    v || (props.service.widget?.[f.key] === SECRET_MASK ? SECRET_MASK : ""),
                  )
                }
              />
            ) : (
              <TextInput
                type={f.number ? "number" : "text"}
                value={s.widget![f.key] == null ? "" : String(s.widget![f.key])}
                onChange={(v) => setWidget(f.key, f.number ? (v === "" ? "" : Number(v)) : v)}
              />
            )}
          </Field>
        ))}
      {s.widget && (
        <Field label="Card width">
          <Segmented
            value={String(s.widget.span ?? 2)}
            options={[
              ["1", "1 column"],
              ["2", "2 columns"],
              ["3", "3"],
              ["4", "4"],
            ]}
            onChange={(v) => setWidget("span", Number(v))}
          />
        </Field>
      )}
    </Dialog>
  );
}

// ── Group ────────────────────────────────────────────────────────────────

export function GroupDialog(props: {
  group: Group;
  isNew: boolean;
  onSave: (g: Group) => void;
  onClose: () => void;
}) {
  const [g, setG] = useState(props.group);
  return (
    <Dialog
      title={props.isNew ? "Add group" : "Edit group"}
      onClose={props.onClose}
      footer={
        <>
          <span class="spacer" />
          <button class="btn" onClick={props.onClose}>
            Cancel
          </button>
          <button class="btn btn-primary" disabled={!g.name.trim()} onClick={() => props.onSave(g)}>
            {props.isNew ? "Add" : "Done"}
          </button>
        </>
      }
    >
      <Field label="Name">
        <TextInput value={g.name} onChange={(v) => setG({ ...g, name: v })} autofocus />
      </Field>
      <Field
        label="Width"
        hint="Auto sizes the group to its services and packs small groups side by side."
      >
        <Segmented
          value={String(g.columns ?? 0)}
          options={[["0", "Auto"], "1", "2", "3", "4", "5", "6"].map((o) =>
            Array.isArray(o) ? (o as [string, string]) : ([o, o] as [string, string]),
          )}
          onChange={(v) => setG({ ...g, columns: Number(v) || undefined })}
        />
      </Field>
      <Toggle
        label="Collapsed by default"
        checked={g.collapsed}
        onChange={(v) => setG({ ...g, collapsed: v })}
      />
    </Dialog>
  );
}

// ── Settings ─────────────────────────────────────────────────────────────

const ACCENTS = ["#2563ff", "#ff3b1f", "#22c55e", "#f59e0b", "#a855f7", "#14b8a6", "#ffffff"];

export function SettingsDialog(props: {
  config: Config;
  onChange: (c: Config) => void;
  onClose: () => void;
}) {
  const { config: c, onChange } = props;
  const [tab, setTab] = useState<"general" | "look" | "header" | "bookmarks" | "alerts">("look");
  const theme = (patch: Partial<Theme>) => onChange({ ...c, theme: { ...c.theme, ...patch } });
  const header = (patch: Partial<Config["header"]>) =>
    onChange({ ...c, header: { ...c.header, ...patch } });
  const system = (patch: Partial<Config["header"]["system"]>) =>
    header({ system: { ...c.header.system, ...patch } });
  const search = (patch: Partial<Config["header"]["search"]>) =>
    header({ search: { ...c.header.search, ...patch } });

  return (
    <Dialog title="Settings" onClose={props.onClose} wide>
      <Segmented
        value={tab}
        options={[
          ["look", "Appearance"],
          ["general", "General"],
          ["header", "Header"],
          ["bookmarks", "Bookmarks"],
          ["alerts", "Alerts"],
        ]}
        onChange={setTab}
      />
      <p class="settings-note">Changes preview live. Save from the toolbar to keep them.</p>

      {tab === "look" && (
        <div class="stack">
          <Field label="Theme">
            <Segmented
              value={c.theme.mode}
              options={[
                ["dark", "Dark"],
                ["light", "Light"],
                ["auto", "System"],
              ]}
              onChange={(v) => theme({ mode: v })}
            />
          </Field>
          <Field label="Accent">
            <div class="swatches">
              {ACCENTS.map((a) => (
                <button
                  key={a}
                  class={`swatch ${c.theme.accent.toLowerCase() === a ? "on" : ""}`}
                  style={{ background: a }}
                  onClick={() => theme({ accent: a })}
                  aria-label={a}
                />
              ))}
              <input
                type="color"
                value={c.theme.accent}
                onInput={(e) => theme({ accent: e.currentTarget.value })}
                aria-label="Custom accent"
              />
            </div>
          </Field>
          <Field label="Typeface">
            <Segmented
              value={c.theme.font}
              options={[
                ["sans", "Swiss"],
                ["serif", "Editorial"],
                ["mono", "Mono"],
              ]}
              onChange={(v) => theme({ font: v })}
            />
          </Field>
          <Field label="Cards">
            <Segmented
              value={c.theme.cards}
              options={[
                ["outline", "Outline"],
                ["filled", "Filled"],
                ["glass", "Glass"],
              ]}
              onChange={(v) => theme({ cards: v })}
            />
          </Field>
          <div class="row">
            <Field label="Density">
              <Segmented
                value={c.theme.density}
                options={[
                  ["comfortable", "Comfortable"],
                  ["compact", "Compact"],
                ]}
                onChange={(v) => theme({ density: v })}
              />
            </Field>
            <Field label="Max columns">
              <Segmented
                value={String(c.theme.columns)}
                options={["2", "3", "4", "5", "6"]}
                onChange={(v) => theme({ columns: Number(v) })}
              />
            </Field>
          </div>
          <Field
            label="Background image"
            hint="A URL, or /images/name.jpg for a file in /config/images."
          >
            <TextInput value={c.theme.background} onChange={(v) => theme({ background: v })} />
          </Field>
          {c.theme.background && (
            <div class="row">
              <Field label={`Dim ${Math.round(c.theme.background_dim * 100)}%`}>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={c.theme.background_dim}
                  onInput={(e) => theme({ background_dim: Number(e.currentTarget.value) })}
                />
              </Field>
              <Field label={`Blur ${c.theme.background_blur}px`}>
                <input
                  type="range"
                  min={0}
                  max={40}
                  value={c.theme.background_blur}
                  onInput={(e) => theme({ background_blur: Number(e.currentTarget.value) })}
                />
              </Field>
            </div>
          )}
          <Field
            label="Custom CSS"
            hint="Appended to the page. The theme is built on CSS variables (--accent, --bg, --card, ...)."
          >
            <textarea
              class="input code"
              rows={5}
              value={c.theme.custom_css}
              spellcheck={false}
              onInput={(e) => theme({ custom_css: e.currentTarget.value })}
            />
          </Field>
        </div>
      )}

      {tab === "general" && (
        <div class="stack">
          <Field label="Page title">
            <TextInput value={c.title} onChange={(v) => onChange({ ...c, title: v })} />
          </Field>
          <Toggle
            label="Open links in a new tab"
            checked={c.open_in_new_tab}
            onChange={(v) => onChange({ ...c, open_in_new_tab: v })}
          />
          <Field label="Status check interval (seconds)">
            <TextInput
              type="number"
              value={String(c.ping_interval)}
              onChange={(v) => onChange({ ...c, ping_interval: Number(v) || 30 })}
            />
          </Field>
        </div>
      )}

      {tab === "header" && (
        <div class="stack">
          <div class="row">
            <Toggle label="Clock" checked={c.header.clock} onChange={(v) => header({ clock: v })} />
            <Toggle
              label="24-hour"
              checked={c.header.clock_24h}
              onChange={(v) => header({ clock_24h: v })}
            />
            <Toggle
              label="Greeting"
              checked={c.header.greeting}
              onChange={(v) => header({ greeting: v })}
            />
          </div>
          <Field label="Your name" hint="Used in the greeting.">
            <TextInput value={c.header.name} onChange={(v) => header({ name: v })} />
          </Field>
          <div class="subhead">Search</div>
          <Toggle
            label="Web search from the search box"
            checked={c.header.search.enabled}
            onChange={(v) => search({ enabled: v })}
          />
          <Field label="Provider">
            <Segmented
              value={c.header.search.provider}
              options={[
                ["google", "Google"],
                ["duckduckgo", "DuckDuckGo"],
                ["bing", "Bing"],
                ["kagi", "Kagi"],
                ["custom", "Custom"],
              ]}
              onChange={(v) => search({ provider: v })}
            />
          </Field>
          {c.header.search.provider === "custom" && (
            <Field
              label="Search URL"
              hint="The query is appended, e.g. https://search.example.com/?q="
            >
              <TextInput value={c.header.search.url} onChange={(v) => search({ url: v })} />
            </Field>
          )}
          <div class="subhead">Server stats</div>
          <Toggle
            label="Show server stats"
            checked={c.header.system.enabled}
            onChange={(v) => system({ enabled: v })}
          />
          <div class="row">
            <Toggle
              label="CPU"
              checked={c.header.system.cpu}
              onChange={(v) => system({ cpu: v })}
            />
            <Toggle
              label="Memory"
              checked={c.header.system.memory}
              onChange={(v) => system({ memory: v })}
            />
            <Toggle
              label="Temperature"
              checked={c.header.system.temperature}
              onChange={(v) => system({ temperature: v })}
            />
            <Toggle
              label="Uptime"
              checked={c.header.system.uptime}
              onChange={(v) => system({ uptime: v })}
            />
          </div>
          <Field
            label="Disks"
            hint="Comma-separated paths inside the container. Mount host drives read-only to watch them."
          >
            <TextInput
              value={c.header.system.disks.join(", ")}
              onChange={(v) =>
                system({
                  disks: v
                    .split(",")
                    .map((d) => d.trim())
                    .filter(Boolean),
                })
              }
            />
          </Field>
        </div>
      )}

      {tab === "bookmarks" && <BookmarksEditor config={c} onChange={onChange} />}
      {tab === "alerts" && (
        <AlertsEditor
          alerts={c.alerts}
          onChange={(patch) => onChange({ ...c, alerts: { ...c.alerts, ...patch } })}
        />
      )}
    </Dialog>
  );
}

function BookmarksEditor({ config, onChange }: { config: Config; onChange: (c: Config) => void }) {
  const groups = config.bookmarks;
  const update = (next: Config["bookmarks"]) => onChange({ ...config, bookmarks: next });
  const patchGroup = (gi: number, patch: Partial<Config["bookmarks"][number]>) =>
    update(groups.map((g, i) => (i === gi ? { ...g, ...patch } : g)));

  return (
    <div class="stack">
      {groups.map((g, gi) => (
        <div class="bm-group" key={gi}>
          <div class="bm-row">
            <TextInput
              value={g.name}
              onChange={(v) => patchGroup(gi, { name: v })}
              placeholder="Group"
            />
            <button
              class="icon-btn"
              title="Remove group"
              onClick={() => update(groups.filter((_, i) => i !== gi))}
            >
              <Trash2 size={14} />
            </button>
          </div>
          {g.links.map((l, li) => {
            const patchLink = (patch: Partial<typeof l>) =>
              patchGroup(gi, { links: g.links.map((x, i) => (i === li ? { ...x, ...patch } : x)) });
            return (
              <div class="bm-row bm-link" key={li}>
                <TextInput
                  value={l.name}
                  onChange={(v) => patchLink({ name: v })}
                  placeholder="Name"
                />
                <TextInput
                  value={l.url}
                  onChange={(v) => patchLink({ url: v })}
                  placeholder="https://"
                />
                <TextInput
                  value={l.abbr}
                  onChange={(v) => patchLink({ abbr: v })}
                  placeholder="Abbr"
                />
                <button
                  class="icon-btn"
                  title="Remove link"
                  onClick={() => patchGroup(gi, { links: g.links.filter((_, i) => i !== li) })}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            );
          })}
          <button
            class="btn btn-ghost"
            onClick={() => patchGroup(gi, { links: [...g.links, { name: "", url: "" }] })}
          >
            <Plus size={14} /> Link
          </button>
        </div>
      ))}
      <button class="btn" onClick={() => update([...groups, { name: "Links", links: [] }])}>
        <Plus size={14} /> Bookmark group
      </button>
    </div>
  );
}

function AlertsEditor(props: { alerts: Alerts; onChange: (patch: Partial<Alerts>) => void }) {
  const { alerts: a, onChange } = props;
  const [test, setTest] = useState<{ tone: string; text: string } | null>(null);
  const [recent, setRecent] = useState<AlertsResponse | null>(null);
  useEffect(() => {
    api.alerts().then(setRecent, () => {});
  }, []);

  const sendTest = async () => {
    setTest({ tone: "", text: "Sending…" });
    try {
      await api.testAlert(a);
      setTest({ tone: "good", text: "Sent. Check your notifications." });
    } catch (e) {
      setTest({ tone: "bad", text: e instanceof Error ? e.message : String(e) });
    }
  };

  // An ongoing problem is listed once, as ongoing, not again as an event.
  const ongoing = new Set((recent?.open ?? []).map((e) => `${e.title}|${e.at}`));
  const events = (recent?.history ?? [])
    .filter((e) => !ongoing.has(`${e.title}|${e.at}`))
    .slice(0, 8);
  return (
    <div class="stack">
      <Field
        label="Apprise URL"
        hint="An Apprise API notify URL for a saved config key, e.g. http://apprise-api:8000/notify/foyer. Empty turns alerts off."
      >
        <TextInput
          value={a.apprise_url}
          onChange={(v) => onChange({ apprise_url: v })}
          placeholder="http://apprise-api:8000/notify/foyer"
        />
      </Field>
      <div class="row">
        <Field label="Tag" hint="Optional: only notify Apprise services with this tag.">
          <TextInput value={a.tag} onChange={(v) => onChange({ tag: v })} />
        </Field>
        <Field
          label="Down after (checks)"
          hint="Failed checks in a row before a service counts as down."
        >
          <TextInput
            type="number"
            value={String(a.down_after)}
            onChange={(v) => onChange({ down_after: Number(v) || 2 })}
          />
        </Field>
      </div>
      <div class="alert-test">
        <button class="btn" onClick={sendTest} disabled={!a.apprise_url}>
          Send a test notification
        </button>
        {test && <span class={`alert-test-result ${test.tone}`}>{test.text}</span>}
      </div>

      <div class="subhead">Notify me when</div>
      <Toggle
        label="A dashboard service goes down or turns unhealthy"
        checked={a.services}
        onChange={(v) => onChange({ services: v })}
      />
      <Toggle
        label="Any other container crashes, restarts in a loop or turns unhealthy"
        checked={a.containers}
        onChange={(v) => onChange({ containers: v })}
      />
      <Toggle
        label="A backup (Keep or Kopia) is overdue or failing"
        checked={a.backups}
        onChange={(v) => onChange({ backups: v })}
      />
      <Toggle
        label="A Syncthing folder has errors"
        checked={a.sync}
        onChange={(v) => onChange({ sync: v })}
      />
      <Toggle
        label="A proxy certificate is close to expiry"
        checked={a.certificates}
        onChange={(v) => onChange({ certificates: v })}
      />
      <p class="field-hint">
        You get one message when something goes wrong and one when it recovers. Keep or Kopia,
        Syncthing and the reverse proxy (Gatehouse or NPM) are checked every 5 minutes through their
        widgets.
      </p>

      {recent && (recent.open.length > 0 || events.length > 0) && (
        <>
          <div class="subhead">Recent</div>
          <ul class="w-list alert-history">
            {recent.open.map((e, i) => (
              <li key={`o${i}`}>
                <span class={`dot ${e.level === "failure" ? "bad" : "warn"}`} />
                <span class="w-row-text">
                  <span class="w-row-name">{e.title}</span>
                  <span class="w-row-sub">ongoing</span>
                </span>
                <span class="w-row-meta">{timeAgo(new Date(e.at))}</span>
              </li>
            ))}
            {events.map((e, i) => (
              <li key={i}>
                <span
                  class={`dot ${e.level === "success" ? "good" : e.level === "failure" ? "bad" : "warn"}`}
                />
                <span class="w-row-text">
                  <span class="w-row-name">{e.title}</span>
                  {e.error && <span class="w-row-sub bad">Not delivered: {e.error}</span>}
                </span>
                <span class="w-row-meta">{timeAgo(new Date(e.at))}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
