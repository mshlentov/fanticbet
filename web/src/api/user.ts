import { api } from "./client";
import type { MeResponse, TransactionsPage } from "./types";

// updateProfile — частичное обновление профиля (display_name, avatar_url).
// nil-поля бэкенд оставляет без изменений; возвращает обновлённый профиль.
export function updateProfile(patch: {
  display_name?: string;
  avatar_url?: string | null;
}): Promise<MeResponse> {
  return api.patch<MeResponse>("/me", patch);
}

// listTransactions — история движений по кошельку с пагинацией.
export function listTransactions(page = 1): Promise<TransactionsPage> {
  return api.get<TransactionsPage>(`/me/transactions?page=${page}`);
}
