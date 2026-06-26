import { api } from "./client";
import type { League, LeaguesResponse } from "./types";

// Админ-эндпоинты M6: кастомные события и корректировка баланса. Все запросы
// за middleware.AuthRequired+AdminRequired — токен подставит api-обёртка.

// AdminOutcomeInput — исход при создании. odds — строка: бэкенд хранит
// NUMERIC(8,3) и принимает коэффициент строкой, чтобы не терять точность.
export type AdminOutcomeInput = {
  label: string;
  odds: string;
};

// CreateCustomEventInput — тело POST /admin/events.
export type CreateCustomEventInput = {
  title: string;
  starts_at: string; // RFC3339 (new Date(...).toISOString())
  market: {
    question?: string | null;
    outcomes: AdminOutcomeInput[];
  };
};

// createCustomEvent — создать кастомное событие с одним CUSTOM-рынком.
export function createCustomEvent(input: CreateCustomEventInput): Promise<unknown> {
  return api.post("/admin/events", input);
}

// cancelEvent — отменить событие (void всех ставок с возвратом). PATCH со
// status='cancelled' — единственное действие отмены на бэке.
export function cancelEvent(id: number): Promise<unknown> {
  return api.patch(`/admin/events/${id}`, { status: "cancelled" });
}

// settleEvent — рассчитать событие по победившему исходу (won → выплата,
// остальные → проигрыш).
export function settleEvent(id: number, winningOutcomeId: number): Promise<unknown> {
  return api.post(`/admin/events/${id}/settle`, { winning_outcome_id: winningOutcomeId });
}

// AdjustBalanceResponse — новый баланс после корректировки.
export type AdjustBalanceResponse = {
  balance: number;
};

// adjustBalance — ручная корректировка баланса (amount может быть отрицательным).
export function adjustBalance(
  userId: number,
  amount: number,
  reason: string,
): Promise<AdjustBalanceResponse> {
  return api.post<AdjustBalanceResponse>(`/admin/users/${userId}/adjust`, {
    amount,
    reason,
  });
}

// --- Чемпионаты (лиги, M8) ---

// listAdminLeagues — список чемпионатов для админки (с датами). Фильтр по виду
// спорта опционален.
export function listAdminLeagues(sportSlug?: string): Promise<LeaguesResponse> {
  const qs = sportSlug ? `?sport_slug=${encodeURIComponent(sportSlug)}` : "";
  return api.get<LeaguesResponse>(`/admin/leagues${qs}`);
}

// createLeague — создать чемпионат, возвращает созданную лигу.
export function createLeague(name: string, sportSlug: string): Promise<League> {
  return api.post<League>("/admin/leagues", { name, sport_slug: sportSlug });
}

// updateLeague — переименовать / сменить спорт. Поля опциональны.
export function updateLeague(
  id: number,
  patch: { name?: string; sport_slug?: string },
): Promise<unknown> {
  return api.patch(`/admin/leagues/${id}`, patch);
}

// deleteLeague — удалить чемпионат. 409, если есть привязанные события.
export function deleteLeague(id: number): Promise<unknown> {
  return api.delete(`/admin/leagues/${id}`);
}

// --- Спортивные матчи (source='manual', M8) ---

// MatchOutcomeInput — исход рынка матча. odds — строка ради точности NUMERIC.
export type MatchOutcomeInput = {
  code: string;
  label: string;
  odds: string;
};

// MatchMarketInput — рынок матча: ML (home/draw/away) или TOTALS (over/under,
// с линией). line — строка NUMERIC(6,2); только для TOTALS.
export type MatchMarketInput = {
  type: "ML" | "TOTALS";
  line?: string | null;
  outcomes: MatchOutcomeInput[];
};

// CreateMatchInput — тело POST /admin/matches.
export type CreateMatchInput = {
  title: string;
  league_id: number;
  starts_at: string; // RFC3339
  home: string;
  away: string;
  markets: MatchMarketInput[];
};

// createMatch — создать спортивный матч (source='manual') с рынками и кэфами.
export function createMatch(input: CreateMatchInput): Promise<unknown> {
  return api.post("/admin/matches", input);
}

// cancelMatch — отменить матч (void ставок с возвратом). Как и у событий —
// PATCH со status='cancelled'.
export function cancelMatch(id: number): Promise<unknown> {
  return api.patch(`/admin/matches/${id}`, { status: "cancelled" });
}

// setMatchScores — ввести финальный счёт → авторасчёт ML+TOTALS → settled.
export function setMatchScores(id: number, home: number, away: number): Promise<unknown> {
  return api.post(`/admin/matches/${id}/scores`, { home, away });
}

// setMatchLive — ручной перевод upcoming → live (рынки suspended, ставки закрыты).
export function setMatchLive(id: number): Promise<unknown> {
  return api.post(`/admin/matches/${id}/status`, { status: "live" });
}
