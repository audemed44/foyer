import type {
  ActionResult,
  Alerts,
  AlertsResponse,
  Config,
  ConfigResponse,
  DockerContainer,
  StatusMap,
  DiscoverResponse,
  DropItem,
  DropResponse,
  DropTarget,
  SystemStats,
  Topology,
} from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: "same-origin", ...init });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      message = (await res.json()).error ?? message;
    } catch {
      // not JSON
    }
    throw new ApiError(message, res.status);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

const json = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

export const api = {
  config: () => request<ConfigResponse>("/api/config"),
  editableConfig: () => request<Config>("/api/config/edit"),
  saveConfig: (config: Config) => request<Config>("/api/config", json("PUT", config)),
  status: () => request<StatusMap>("/api/status"),
  system: () => request<SystemStats>("/api/system"),
  widget: <T>(id: string) => request<T>(`/api/widgets/${encodeURIComponent(id)}`),
  widgetAction: (id: string, url: string) =>
    request<ActionResult>(`/api/widgets/${encodeURIComponent(id)}/action`, json("POST", { url })),
  widgetActionStatus: (id: string, status: string) =>
    request<ActionResult>(
      `/api/widgets/${encodeURIComponent(id)}/action?status=${encodeURIComponent(status)}`,
    ),
  icons: () => request<string[]>("/api/icons"),
  containers: () => request<DockerContainer[]>("/api/containers"),
  discover: () => request<DiscoverResponse>("/api/discover"),
  widgetImage: (serviceId: string, path: string) =>
    `/api/widgets/${encodeURIComponent(serviceId)}/image?path=${encodeURIComponent(path)}`,
  logsUrl: (name: string, tail = 500) =>
    `/api/containers/${encodeURIComponent(name)}/logs?tail=${tail}`,
  topology: () => request<Topology>("/api/topology"),
  alerts: () => request<AlertsResponse>("/api/alerts"),
  testAlert: (alerts: Alerts) =>
    request<{ message: string }>("/api/alerts/test", json("POST", alerts)),
  drop: () => request<DropResponse>("/api/drop"),
  dropTargets: () => request<DropTarget[]>("/api/drop/targets"),
  deleteDrop: (id: string) =>
    request<void>(`/api/drop/${encodeURIComponent(id)}`, { method: "DELETE" }),
  sendDrop: (id: string, service: string) =>
    request<{ message: string; url?: string }>(
      `/api/drop/${encodeURIComponent(id)}/send`,
      json("POST", { service }),
    ),
  dropFileUrl: (id: string, download = false) =>
    `/api/drop/${encodeURIComponent(id)}/file${download ? "?download=1" : ""}`,
};

/**
 * Saves a note, link or files to Drop. Uses XHR rather than fetch for upload
 * progress (0–1).
 */
export function uploadDrop(form: FormData, onProgress?: (p: number) => void): Promise<DropItem[]> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/drop");
    xhr.responseType = "json";
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve(xhr.response as DropItem[]);
      else reject(new ApiError(xhr.response?.error ?? `HTTP ${xhr.status}`, xhr.status));
    };
    xhr.onerror = () => reject(new ApiError("Upload failed — check your connection", 0));
    xhr.send(form);
  });
}
