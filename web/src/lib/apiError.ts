import { ApiError } from "../api/client";

// errMessage — текст ошибки API для тоста (единый формат бэкенда
// {"error":{"code","message"}}). Для прочих ошибок — переданный fallback.
export function errMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}
