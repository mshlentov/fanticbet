import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { getLeaderboard } from "../api/stats";
import type {
  LeaderboardMetric,
  LeaderboardPeriod,
  LeaderboardRow,
} from "../api/types";
import { useAuth } from "../hooks/useAuth";
import { EmptyState, ErrorState, LoadingState } from "../components/states";
import { avatarBg, fmtCoins, initials } from "../lib/format";

const PERIODS: { value: LeaderboardPeriod; label: string }[] = [
  { value: "week", label: "Неделя" },
  { value: "month", label: "Месяц" },
  { value: "all", label: "Всё время" },
];

const METRICS: { value: LeaderboardMetric; label: string }[] = [
  { value: "profit", label: "Прибыль" },
  { value: "roi", label: "ROI" },
];

// Медали для топ-3 (золото/серебро/бронза), дальше — без подложки.
const MEDALS = ["var(--gold)", "#9aa3b2", "#c2855a"];

// LeaderboardPage — топ прогнозистов с фильтрами по периоду (week/month/all)
// и метрике сортировки (profit/roi). Данные публичные (без авторизации).
export function LeaderboardPage() {
  const [period, setPeriod] = useState<LeaderboardPeriod>("all");
  const [metric, setMetric] = useState<LeaderboardMetric>("profit");

  const query = useQuery({
    queryKey: ["leaderboard", { period, metric }],
    queryFn: () => getLeaderboard(period, metric),
  });

  const rows = query.data?.items ?? [];

  return (
    <section style={{ maxWidth: 760, margin: "0 auto" }}>
      <h1 style={{ margin: "0 0 16px", fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
        Лидерборд
      </h1>

      {/* Фильтры: период (чипы) слева, метрика (сегмент) справа */}
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 10,
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 16,
        }}
      >
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {PERIODS.map((p) => (
            <button
              key={p.value}
              type="button"
              className={`fb-chip${period === p.value ? " is-active" : ""}`}
              onClick={() => setPeriod(p.value)}
            >
              {p.label}
            </button>
          ))}
        </div>
        <div className="fb-segment">
          {METRICS.map((m) => (
            <button
              key={m.value}
              type="button"
              className={`fb-segment-btn${metric === m.value ? " is-active" : ""}`}
              onClick={() => setMetric(m.value)}
            >
              {m.label}
            </button>
          ))}
        </div>
      </div>

      {query.isPending && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      {query.isSuccess && rows.length === 0 && (
        <EmptyState>
          Пока никто не попал в топ — нужно сделать достаточно ставок.
        </EmptyState>
      )}

      {query.isSuccess && rows.length > 0 && (
        <div className="fb-card fb-anim" style={{ overflow: "hidden", padding: 0 }}>
          {/* Шапка таблицы */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 12,
              padding: "10px 18px",
              borderBottom: "1px solid var(--border)",
              fontSize: 11,
              fontWeight: 700,
              color: "var(--text3)",
              textTransform: "uppercase",
              letterSpacing: "0.06em",
            }}
          >
            <span style={{ width: 32 }}>#</span>
            <span style={{ flex: 1 }}>Игрок</span>
            <span style={{ width: 90, textAlign: "right" }}>
              {metric === "profit" ? "Прибыль" : "ROI"}
            </span>
            <span className="fb-lb-extra" style={{ width: 64, textAlign: "right" }}>
              Ставок
            </span>
            <span className="fb-lb-extra" style={{ width: 64, textAlign: "right" }}>
              Винрейт
            </span>
          </div>
          {rows.map((row, i) => (
            <Row key={row.user_id} row={row} rank={i + 1} metric={metric} />
          ))}
        </div>
      )}
    </section>
  );
}

function Row({
  row,
  rank,
  metric,
}: {
  row: LeaderboardRow;
  rank: number;
  metric: LeaderboardMetric;
}) {
  const { user } = useAuth();
  const isMe = user?.id === row.user_id;

  // Значение метрики: profit — фантики со знаком, roi — проценты.
  const value =
    metric === "profit"
      ? `${row.profit >= 0 ? "+" : "−"}${fmtCoins(Math.abs(row.profit))}`
      : `${row.roi >= 0 ? "+" : "−"}${Math.abs(row.roi * 100).toFixed(1)}%`;
  const valueColor =
    metric === "profit"
      ? row.profit >= 0
        ? "var(--green)"
        : "var(--red)"
      : row.roi >= 0
        ? "var(--green)"
        : "var(--red)";

  const winRate = row.total_bets
    ? `${Math.round((row.won_bets / row.total_bets) * 100)}%`
    : "—";

  return (
    <Link
      to={`/users/${row.user_id}`}
      className="fb-lb-row"
      style={{
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: "10px 18px",
        borderBottom: "1px solid var(--border)",
        background: isMe ? "var(--accent-soft)" : "transparent",
        color: "inherit",
        textDecoration: "none",
      }}
    >
      <span style={{ width: 32, flexShrink: 0 }}>
        <span
          style={{
            display: "inline-grid",
            placeItems: "center",
            width: 24,
            height: 24,
            borderRadius: "50%",
            fontSize: 12,
            fontWeight: 800,
            background: rank <= 3 ? MEDALS[rank - 1] : "transparent",
            color: rank <= 3 ? "#fff" : "var(--text3)",
          }}
        >
          {rank}
        </span>
      </span>
      <span style={{ flex: 1, display: "flex", alignItems: "center", gap: 10, minWidth: 0 }}>
        <Avatar name={row.display_name} url={row.avatar_url} />
        <span
          style={{
            fontSize: 13.5,
            fontWeight: 700,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {row.display_name}
          {isMe && <span style={{ color: "var(--text3)", fontWeight: 600 }}> (вы)</span>}
        </span>
      </span>
      <span style={{ width: 90, textAlign: "right", fontSize: 14, fontWeight: 800, color: valueColor }}>
        {value}
      </span>
      <span
        className="fb-lb-extra"
        style={{ width: 64, textAlign: "right", fontSize: 13, color: "var(--text2)", fontWeight: 600 }}
      >
        {row.total_bets}
      </span>
      <span
        className="fb-lb-extra"
        style={{ width: 64, textAlign: "right", fontSize: 13, color: "var(--text2)", fontWeight: 600 }}
      >
        {winRate}
      </span>
    </Link>
  );
}

// Avatar — круглый аватар: либо картинка, либо инициалы на стабильном фоне.
function Avatar({ name, url }: { name: string; url: string | null }) {
  if (url) {
    return (
      <img
        src={url}
        alt={name}
        style={{ width: 30, height: 30, borderRadius: "50%", objectFit: "cover", flexShrink: 0 }}
      />
    );
  }
  return (
    <span
      style={{
        width: 30,
        height: 30,
        borderRadius: "50%",
        background: avatarBg(name),
        color: "#fff",
        fontSize: 11,
        fontWeight: 700,
        display: "grid",
        placeItems: "center",
        flexShrink: 0,
      }}
    >
      {initials(name)}
    </span>
  );
}
