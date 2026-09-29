export type Widget = { type: string; span?: number; [key: string]: unknown };

export type Service = {
  id: string;
  name: string;
  url?: string;
  description?: string;
  icon?: string;
  ping?: string;
  container?: string;
  widget?: Widget;
};

export type Group = {
  id: string;
  name: string;
  columns?: number;
  collapsed: boolean;
  services: Service[];
};

export type Bookmark = { name: string; url: string; abbr?: string; icon?: string };
export type BookmarkGroup = { name: string; links: Bookmark[] };

export type Theme = {
  mode: "dark" | "light" | "auto";
  accent: string;
  font: "mono" | "sans" | "serif";
  cards: "outline" | "filled" | "glass";
  density: "comfortable" | "compact";
  columns: number;
  background: string;
  background_dim: number;
  background_blur: number;
  custom_css: string;
};

export type Config = {
  title: string;
  open_in_new_tab: boolean;
  ping_interval: number;
  theme: Theme;
  header: {
    greeting: boolean;
    name: string;
    clock: boolean;
    clock_24h: boolean;
    search: {
      enabled: boolean;
      provider: "google" | "duckduckgo" | "bing" | "kagi" | "custom";
      url: string;
    };
    system: {
      enabled: boolean;
      cpu: boolean;
      memory: boolean;
      temperature: boolean;
      uptime: boolean;
      disks: string[];
    };
  };
  groups: Group[];
  bookmarks: BookmarkGroup[];
  ignored_containers: string[];
};

export type ConfigResponse = {
  config: Config;
  error?: string;
  widget_types: string[];
};

export type Ping = {
  state: "up" | "down";
  code?: number;
  latency_ms?: number;
  error?: string;
  checked_at: number;
};

export type ContainerState = { name: string; state: string; status: string; health?: string };
export type ServiceStatus = { ping?: Ping; container?: ContainerState };
export type StatusMap = Record<string, ServiceStatus>;

export type SystemStats = {
  cpu: { percent: number; history: number[]; cores: number; load: number[] | null };
  memory: { percent: number; used: number; total: number; history: number[] };
  uptime: number;
  temperature: number | null;
  disks: { path: string; used: number; total: number; percent: number }[];
};

export type DockerStats = {
  cpu: number | null;
  mem_used: number;
  mem_limit: number;
  net_rx: number;
  net_tx: number;
  pids: number;
};

export type DockerContainer = {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  health?: string;
  created: number;
  project?: string;
  stats?: DockerStats;
};

export type LogLine = { t?: string; s: "out" | "err"; m: string };

export type Suggestion = {
  container: string;
  image: string;
  name: string;
  description?: string;
  icon?: string;
  url?: string;
  url_guessed?: boolean;
  ping?: string;
  group?: string;
  labelled?: boolean;
  widget?: Widget;
};

export type WidgetSuggestion = { service: string; name: string; widget: Widget };
export type DiscoverResponse = { containers: Suggestion[]; widgets: WidgetSuggestion[] };

export type DropItem = {
  id: string;
  kind: "text" | "link" | "file";
  text?: string;
  url?: string;
  title?: string;
  file?: { name: string; size: number; type: string };
  created: number;
};
export type DropResponse = { items: DropItem[]; max_file: number; usage: number };
export type DropTarget = {
  service: string;
  name: string;
  icon?: string;
  label: string;
  types: string[];
};
