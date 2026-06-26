import type { Bet, BetStatus } from "../api/types";
import { fmtCoins } from "./format";

// Подпись и цвета бейджа статуса ставки (как в макете).
export const BET_STATUS_META: Record<
  BetStatus,
  { label: string; bg: string; color: string }
> = {
  pending: { label: "В ожидании", bg: "var(--amber-soft)", color: "var(--amber)" },
  won: { label: "Выиграна", bg: "var(--green-soft)", color: "var(--green)" },
  lost: { label: "Проиграна", bg: "var(--red-soft)", color: "var(--red)" },
  void: { label: "Возврат", bg: "var(--surface2)", color: "var(--text2)" },
};

// betEventTitle — заголовок события в строке истории ставок. Для матчей с
// командами — «Команда A — Команда B», иначе название события. Если обогащение
// не пришло (старые данные / ответ POST /bets) — откатываемся на «Событие #id».
export function betEventTitle(bet: Bet): string {
  if (bet.event_home && bet.event_away) {
    return `${bet.event_home} — ${bet.event_away}`;
  }
  return bet.event_title || `Событие #${bet.event_id}`;
}

// betOutcomeLabel — название исхода в строке истории ставок, с откатом на
// «Исход #id», если label не пришёл.
export function betOutcomeLabel(bet: Bet): string {
  return bet.outcome_label || `Исход #${bet.outcome_id}`;
}

// betResultLine — итоговая строка ставки (потенциал / выигрыш / проигрыш / возврат).
export function betResultLine(bet: Bet): { text: string; color: string } {
  switch (bet.status) {
    case "won":
      return { text: `+${fmtCoins(bet.potential_payout - bet.stake)}`, color: "var(--green)" };
    case "lost":
      return { text: `−${fmtCoins(bet.stake)}`, color: "var(--red)" };
    case "void":
      return { text: `возврат ${fmtCoins(bet.stake)}`, color: "var(--text2)" };
    default:
      return { text: `→ ${fmtCoins(bet.potential_payout)}`, color: "var(--text)" };
  }
}
