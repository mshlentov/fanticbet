import type { MouseEvent } from "react";

import { useBetslip } from "../hooks/useBetslip";
import { useToast } from "../hooks/useToast";
import { fmtOdds } from "../lib/format";

type Props = {
  outcomeId: number;
  label: string;
  odds: number;
  eventId: number;
  eventTitle: string;
  marketLabel: string;
  closed: boolean;
  variant?: "card" | "row";
};

// OutcomeButton — кнопка коэффициента. Клик добавляет/убирает исход из купона
// (один исход на событие). Если ставки закрыты — показываем тост и не реагируем.
export function OutcomeButton({
  outcomeId,
  label,
  odds,
  eventId,
  eventTitle,
  marketLabel,
  closed,
  variant = "card",
}: Props) {
  const { isSelected, toggle } = useBetslip();
  const { toast } = useToast();
  const selected = isSelected(outcomeId);

  const handleClick = (e: MouseEvent) => {
    e.stopPropagation(); // не открывать карточку события при клике по коэффициенту
    if (closed) {
      toast("Ставки на это событие закрыты", "err");
      return;
    }
    toggle({ outcomeId, eventId, eventTitle, marketLabel, outcomeLabel: label, odds });
  };

  const base = variant === "row" ? "fb-outcome-row" : "fb-outcome";
  const cls = [base, selected && "is-selected", closed && !selected && "is-closed"]
    .filter(Boolean)
    .join(" ");

  if (variant === "row") {
    return (
      <button type="button" className={cls} onClick={handleClick}>
        <span style={{ fontSize: 13.5, fontWeight: 600, textAlign: "left" }}>
          {label}
        </span>
        <span style={{ fontSize: 17, fontWeight: 800 }}>{fmtOdds(odds)}</span>
      </button>
    );
  }

  return (
    <button type="button" className={cls} onClick={handleClick}>
      <span className="label">{label}</span>
      <span className="odds">{fmtOdds(odds)}</span>
    </button>
  );
}
