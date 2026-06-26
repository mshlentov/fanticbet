// FeaturedToggle — кнопка управления популярностью события в админке: помечает
// событие звездой («в популярное») или снимает метку («убрать»). Reusable —
// используется в списке матчей (MatchesSection) и кастомных событий (AdminPage).
export function FeaturedToggle({
  isFeatured,
  disabled,
  onToggle,
}: {
  isFeatured: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className="fb-icon-btn"
      disabled={disabled}
      onClick={onToggle}
      aria-pressed={isFeatured}
      title={isFeatured ? "Убрать из популярных" : "В популярное"}
      style={{
        color: isFeatured ? "var(--accent)" : "var(--text3)",
        flexShrink: 0,
      }}
    >
      {isFeatured ? "★" : "☆"}
    </button>
  );
}
