import { api } from "./client";

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
