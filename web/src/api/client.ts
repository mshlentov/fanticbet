import type { TokenResponse } from "./types";

// Базовый префикс API. В dev запросы идут через Vite-proxy на :8080,
// в prod nginx раздаёт статику и проксирует /api на тот же бэкенд.
const API_BASE = "/api/v1";

// Access-токен живём только в памяти (НЕ в localStorage) — так его не украсть
// через XSS. Refresh-токен лежит в httpOnly-cookie, JS его не видит вовсе.
let accessToken: string | null = null;

// Колбэк «сессия окончательно протухла»: refresh не сработал. Регистрируется
// из AuthContext, чтобы сбросить состояние авторизации (без редиректа —
// навигацией занимается ProtectedRoute).
let onAuthFailure: (() => void) | null = null;

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

export function getAccessToken(): string | null {
  return accessToken;
}

export function setOnAuthFailure(cb: (() => void) | null): void {
  onAuthFailure = cb;
}

// ApiError — ошибка в едином формате бэкенда {"error": {"code","message"}}.
// code позволяет фронту реагировать точечно (insufficient_balance, market_closed…).
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  // auth=false — публичный запрос (login/register/refresh, лента событий).
  auth?: boolean;
};

// tryRefresh пытается обменять refresh-cookie на новый access. Зовём fetch
// напрямую (не через request), чтобы не зациклить авто-refresh на 401.
async function tryRefresh(): Promise<boolean> {
  try {
    const res = await fetch(`${API_BASE}/auth/refresh`, {
      method: "POST",
      credentials: "include",
    });
    if (!res.ok) {
      return false;
    }
    const data = (await res.json()) as TokenResponse;
    accessToken = data.access_token;
    return true;
  } catch {
    return false;
  }
}

// parseResponse разбирает тело и единый формат ошибок. Пустое тело (например,
// 204) возвращается как null.
async function parseResponse<T>(res: Response): Promise<T> {
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;

  if (!res.ok) {
    const err = data?.error;
    throw new ApiError(
      res.status,
      err?.code ?? "internal_error",
      err?.message ?? "Что-то пошло не так",
    );
  }
  return data as T;
}

// request — обёртка над fetch: подставляет Bearer, сериализует JSON, шлёт
// cookie и при 401 на защищённом запросе один раз обновляет access и повторяет.
async function request<T>(
  path: string,
  options: RequestOptions = {},
  isRetry = false,
): Promise<T> {
  const { body, auth = true, headers, ...rest } = options;

  const finalHeaders = new Headers(headers);
  if (body !== undefined) {
    finalHeaders.set("Content-Type", "application/json");
  }
  if (auth && accessToken) {
    finalHeaders.set("Authorization", `Bearer ${accessToken}`);
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...rest,
    headers: finalHeaders,
    credentials: "include", // браузер сам приложит refresh-cookie
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  // 401 на защищённом запросе → пробуем обновить access (один раз) и повторить.
  if (res.status === 401 && auth && !isRetry) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      return request<T>(path, options, true);
    }
    if (onAuthFailure) {
      onAuthFailure();
    }
  }

  return parseResponse<T>(res);
}

// api — публичные хелперы по HTTP-методам.
export const api = {
  get: <T>(path: string, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "GET" }),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "POST", body }),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "PATCH", body }),
  delete: <T>(path: string, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "DELETE" }),
};
