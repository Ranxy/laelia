// Origin only — /v1 prefix is fixed by the backend's gRPC-Gateway routing.
const BASE = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/+$/, "");

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const res = await fetch(`${BASE}/v1${path}`, {
    method,
    headers: body != null ? { "Content-Type": "application/json" } : undefined,
    body: body != null ? JSON.stringify(body) : undefined,
    credentials: "include",
  });

  if (!res.ok) {
    const text = await res.text().catch(() => "");
    let message = `${res.status} ${res.statusText}`;
    try {
      const err = JSON.parse(text);
      message = err.message ?? err.error ?? message;
    } catch {
      if (text) message = text;
    }
    throw new Error(message);
  }

  // 204 No Content or empty body
  const contentLength = res.headers.get("content-length");
  if (res.status === 204 || contentLength === "0") {
    return undefined as T;
  }

  return res.json() as Promise<T>;
}

export const api = {
  get<T>(path: string): Promise<T> {
    return request<T>("GET", path);
  },
  post<T>(path: string, body?: unknown): Promise<T> {
    return request<T>("POST", path, body);
  },
};
