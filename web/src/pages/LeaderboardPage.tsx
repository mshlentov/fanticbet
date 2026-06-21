// LeaderboardPage — топ прогнозистов (фильтры period/metric). Бэкенд-агрегаты
// и эндпоинт /leaderboard относятся к вехе M5 — пока on-brand заглушка.
export function LeaderboardPage() {
  return (
    <section style={{ maxWidth: 760, margin: "0 auto" }}>
      <h1 style={{ margin: "0 0 16px", fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
        Лидерборд
      </h1>
      <div className="fb-card fb-anim" style={{ padding: 28, textAlign: "center" }}>
        <div style={{ fontSize: 15, fontWeight: 700, marginBottom: 6 }}>Скоро</div>
        <p style={{ margin: 0, fontSize: 13.5, color: "var(--text3)" }}>
          Таблица топа с фильтрами по периоду и метрике (прибыль / ROI) появится
          в M5 вместе с серверной статистикой.
        </p>
      </div>
    </section>
  );
}
