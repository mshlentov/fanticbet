import type { Market } from "../api/types";

// Человекочитаемые названия видов спорта; для неизвестных — сам slug.
const SPORT_LABELS: Record<string, string> = {
  football: "Футбол",
  basketball: "Баскетбол",
  tennis: "Теннис",
  hockey: "Хоккей",
  custom: "Кастомные",
};

export function sportLabel(slug: string): string {
  return SPORT_LABELS[slug] ?? slug;
}

// marketTitle — заголовок рынка для карточки/страницы события.
export function marketTitle(m: Pick<Market, "type" | "line" | "question">): string {
  if (m.type === "ML") return "Исход матча";
  if (m.type === "TOTALS") return `Тотал ${m.line ?? ""}`.trim();
  return m.question ?? "Спецрынок";
}

// Русское склонение слова «событие» по числу.
export function eventsCountLabel(n: number): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  let word = "событий";
  if (mod10 === 1 && mod100 !== 11) word = "событие";
  else if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) word = "события";
  return `${n} ${word}`;
}

// Порядок статусов для сортировки ленты (live → upcoming → settled/прочее).
export function statusOrder(status: string): number {
  if (status === "live") return 0;
  if (status === "upcoming") return 1;
  return 2;
}
