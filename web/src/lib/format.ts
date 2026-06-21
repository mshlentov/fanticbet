// fmtCoins — целые фантики с разделением разрядов (ru-RU).
export function fmtCoins(n: number): string {
  return Math.round(n).toLocaleString("ru-RU");
}

// fmtOdds — коэффициент всегда с двумя знаками (1.9 → "1.90"). Бэкенд отдаёт
// NUMERIC(8,3) как JSON-строку ("1.950"), поэтому приводим к числу через Number.
export function fmtOdds(odds: number | string): string {
  return Number(odds).toFixed(2);
}

// absTime — дата/время старта события в коротком формате.
export function absTime(iso: string): string {
  return new Date(iso).toLocaleString("ru-RU", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// relTime — относительное время до старта («через 3 ч»), либо статус.
export function relTime(iso: string, status: string): string {
  if (status === "live") return "LIVE";
  if (status === "settled") return "Завершено";
  if (status === "cancelled") return "Отменено";
  const m = Math.max(1, Math.round((new Date(iso).getTime() - Date.now()) / 6e4));
  if (m < 60) return `через ${m} мин`;
  const h = Math.round(m / 60);
  if (h < 48) return `через ${h} ч`;
  return `через ${Math.round(h / 24)} дн`;
}

// initials — инициалы из имени для аватара.
export function initials(name: string): string {
  const parts = name
    .replace(/[^a-zA-Zа-яА-Я0-9 _]/g, "")
    .split(/[ _]+/)
    .filter(Boolean);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return name.slice(0, 2).toUpperCase();
}

// avatarBg — стабильный цвет аватара по имени (как в макете).
export function avatarBg(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360;
  return `oklch(0.55 0.13 ${h})`;
}
