import { api, setAccessToken } from "./client";
import type { MeResponse, TokenResponse } from "./types";

// login — вход по email+паролю. Сохраняет access в память, refresh — в cookie
// (его ставит бэкенд). auth:false — запрос не требует Bearer.
export async function login(email: string, password: string): Promise<void> {
  const res = await api.post<TokenResponse>(
    "/auth/login",
    { email, password },
    { auth: false },
  );
  setAccessToken(res.access_token);
}

// register — регистрация с бонусом. По успеху сразу авторизован (как login).
export async function register(
  email: string,
  password: string,
  displayName: string,
): Promise<void> {
  const res = await api.post<TokenResponse>(
    "/auth/register",
    { email, password, display_name: displayName },
    { auth: false },
  );
  setAccessToken(res.access_token);
}

// logout — отзыв refresh-токена на бэкенде + очистка access в памяти.
export async function logout(): Promise<void> {
  try {
    await api.post("/auth/logout", undefined, { auth: false });
  } finally {
    setAccessToken(null);
  }
}

// getMe — профиль + баланс текущего пользователя. На старте приложения этот же
// запрос «будит» авто-refresh: если access пуст, 401 → refresh по cookie → retry.
export function getMe(): Promise<MeResponse> {
  return api.get<MeResponse>("/me");
}

// Ссылка на OAuth-логин провайдера (yandex | vk). Это редирект-эндпоинт, поэтому
// переход делаем через window.location, а не fetch.
export function oauthLoginUrl(provider: "yandex" | "vk"): string {
  return `/api/v1/auth/${provider}/login`;
}
