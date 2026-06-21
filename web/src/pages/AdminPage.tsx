// AdminPage — управление кастомными событиями (создание, settle/cancel, adjust).
// Админ-эндпоинты относятся к вехе M6 — пока on-brand заглушка.
export function AdminPage() {
  return (
    <section>
      <h1 style={{ margin: "0 0 4px", fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
        Админ-панель
      </h1>
      <p style={{ margin: "0 0 18px", fontSize: 13, color: "var(--text3)" }}>
        Кастомные события и корректировки баланса
      </p>
      <div className="fb-card fb-anim" style={{ padding: 28, textAlign: "center" }}>
        <div style={{ fontSize: 15, fontWeight: 700, marginBottom: 6 }}>Скоро</div>
        <p style={{ margin: 0, fontSize: 13.5, color: "var(--text3)" }}>
          Создание кастомных событий, ручной расчёт и корректировка балансов
          появятся в M6.
        </p>
      </div>
    </section>
  );
}
