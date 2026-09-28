import type {
  Config,
  ConfigResponse,
  DockerContainer,
  StatusMap,
  Suggestion,
  SystemStats,
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
  icons: () => request<string[]>("/api/icons"),
  containers: () => request<DockerContainer[]>("/api/containers"),
  discover: () => request<Suggestion[]>("/api/discover"),
  logsUrl: (name: string, tail = 500) =>
    `/api/containers/${encodeURIComponent(name)}/logs?tail=${tail}`,
};
