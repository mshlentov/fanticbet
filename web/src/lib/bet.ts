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
