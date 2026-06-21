import { createContext, useCallback, useMemo, useState } from "react";
import type { ReactNode } from "react";

// SlipItem — позиция в купоне. Хранит всё нужное для отображения и отправки
// ставки (outcomeId + stake — то, что уходит в POST /bets).
export type SlipItem = {
  outcomeId: number;
  eventId: number;
  eventTitle: string;
  marketLabel: string;
  outcomeLabel: string;
  odds: number;
  stake: string; // строка — поле ввода; парсится при расчёте/отправке
};

export type AddSelection = Omit<SlipItem, "stake">;

export type BetslipContextValue = {
  items: SlipItem[];
  open: boolean;
  isSelected: (outcomeId: number) => boolean;
  // toggle — добавить/убрать исход. На одно событие — один исход (как в макете).
  toggle: (sel: AddSelection) => void;
  remove: (outcomeId: number) => void;
  setStake: (outcomeId: number, stake: string) => void;
  clear: () => void;
  setOpen: (open: boolean) => void;
};

const DEFAULT_STAKE = "100";

// eslint-disable-next-line react-refresh/only-export-components
export const BetslipContext = createContext<BetslipContextValue | null>(null);

export function BetslipProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<SlipItem[]>([]);
  const [open, setOpen] = useState(false);

  const isSelected = useCallback(
    (outcomeId: number) => items.some((i) => i.outcomeId === outcomeId),
    [items],
  );

  const toggle = useCallback((sel: AddSelection) => {
    setItems((prev) => {
      const exists = prev.some((i) => i.outcomeId === sel.outcomeId);
      // Убираем прежний исход того же события (один исход на событие).
      const rest = prev.filter((i) => i.eventId !== sel.eventId);
      if (exists) return rest;
      return [...rest, { ...sel, stake: DEFAULT_STAKE }];
    });
    // Добавление исхода раскрывает купон.
    setOpen((wasOpen) => wasOpen || !items.some((i) => i.outcomeId === sel.outcomeId));
  }, [items]);

  const remove = useCallback((outcomeId: number) => {
    setItems((prev) => prev.filter((i) => i.outcomeId !== outcomeId));
  }, []);

  const setStake = useCallback((outcomeId: number, stake: string) => {
    setItems((prev) =>
      prev.map((i) => (i.outcomeId === outcomeId ? { ...i, stake } : i)),
    );
  }, []);

  const clear = useCallback(() => setItems([]), []);

  const value = useMemo<BetslipContextValue>(
    () => ({ items, open, isSelected, toggle, remove, setStake, clear, setOpen }),
    [items, open, isSelected, toggle, remove, setStake, clear],
  );

  return (
    <BetslipContext.Provider value={value}>{children}</BetslipContext.Provider>
  );
}
