// Набор SVG-иконок из макета. Цвета берут из currentColor / CSS-переменных.

// CoinIcon — фирменная золотая «фантик»-монета с буквой Ф.
export function CoinIcon({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="11" fill="var(--gold)" />
      <circle
        cx="12"
        cy="12"
        r="8"
        fill="none"
        stroke="rgba(80,46,0,0.35)"
        strokeWidth="1.5"
      />
      <text
        x="12"
        y="16.2"
        textAnchor="middle"
        fontSize="11"
        fontWeight="800"
        fill="rgba(80,46,0,0.8)"
        fontFamily="Inter, sans-serif"
      >
        Ф
      </text>
    </svg>
  );
}

// SportIcon — иконка вида спорта по slug (football / basketball / custom).
export function SportIcon({ sport, size = 14 }: { sport: string; size?: number }) {
  if (sport === "basketball") {
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3v18M5.7 5.7c3.4 3.5 3.4 9.1 0 12.6M18.3 5.7c-3.4 3.5-3.4 9.1 0 12.6" />
      </svg>
    );
  }
  if (sport === "football") {
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="9" />
        <polygon
          points="12,8.2 15.6,10.8 14.2,15.2 9.8,15.2 8.4,10.8"
          fill="currentColor"
          stroke="none"
        />
      </svg>
    );
  }
  // custom / прочее — звезда
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M12 3l2.2 6.8L21 12l-6.8 2.2L12 21l-2.2-6.8L3 12l6.8-2.2z"
        fill="currentColor"
      />
    </svg>
  );
}

// SunIcon / MoonIcon — переключатель темы.
export function SunIcon({ size = 17 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="4" fill="currentColor" />
      <path
        d="M12 2v3M12 19v3M2 12h3M19 12h3M4.5 4.5l2.1 2.1M17.4 17.4l2.1 2.1M19.5 4.5l-2.1 2.1M6.6 17.4l-2.1 2.1"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function MoonIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"
        fill="currentColor"
      />
    </svg>
  );
}
