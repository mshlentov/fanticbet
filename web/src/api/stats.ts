import { api } from "./client";
import type {
  LeaderboardMetric,
  LeaderboardPage,
  LeaderboardPeriod,
  PublicProfile,
  UserBetsPage,
} from "./types";

// Социальная часть (M5). Эндпоинты публичные (без авторизации): профиль со
// статистикой, история ставок любого пользователя и лидерборд.

// getPublicProfile — публичный профиль пользователя со статистикой.
export function getPublicProfile(id: number): Promise<PublicProfile> {
  return api.get<PublicProfile>(`/users/${id}`, { auth: false });
}

// listUserBets — публичная история ставок пользователя (фильтр/пагинация).
export function listUserBets(
  id: number,
  status?: string,
  page = 1,
): Promise<UserBetsPage> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  params.set("page", String(page));
  return api.get<UserBetsPage>(`/users/${id}/bets?${params.toString()}`, {
    auth: false,
  });
}

// getLeaderboard — топ прогнозистов по прибыли/ROI за период (кэш бэка 60с).
export function getLeaderboard(
  period: LeaderboardPeriod,
  metric: LeaderboardMetric,
  page = 1,
): Promise<LeaderboardPage> {
  const params = new URLSearchParams({
    period,
    metric,
    page: String(page),
  });
  return api.get<LeaderboardPage>(`/leaderboard?${params.toString()}`, {
    auth: false,
  });
}
