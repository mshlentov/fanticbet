import { api } from "./client";
import type { BetsPage, PlaceBetResponse } from "./types";

// placeBet — размещение ставки (POST /bets). Требует авторизации; возвращает
// созданную ставку и баланс после списания.
export function placeBet(
  outcomeId: number,
  stake: number,
): Promise<PlaceBetResponse> {
  return api.post<PlaceBetResponse>("/bets", {
    outcome_id: outcomeId,
    stake,
  });
}

// listMyBets — мои ставки (GET /me/bets) с фильтром по статусу и пагинацией.
export function listMyBets(status?: string, page = 1): Promise<BetsPage> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  params.set("page", String(page));
  return api.get<BetsPage>(`/me/bets?${params.toString()}`);
}
