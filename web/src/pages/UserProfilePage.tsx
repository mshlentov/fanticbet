import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { getPublicProfile, listUserBets } from "../api/stats";
import type { Bet, PublicProfile } from "../api/types";
import { EmptyState, ErrorState, LoadingState } from "../components/states";
import { BET_STATUS_META, betResultLine } from "../lib/bet";
import { absTime, avatarBg, fmtCoins, fmtOdds, initials } from "../lib/format";

// UserProfilePage — публичный профиль: карточка со статистикой (ставок,
// винрейт, прибыль, ROI) и история ставок пользователя. Данные публичные.
export function UserProfilePage() {
  const { id } = useParams<{ id: string }>();
  const userId = Number(id);
  const valid = Number.isInteger(userId) && userId > 0;

  const profileQuery = useQuery({
    queryKey: ["user-profile", userId],
    queryFn: () => getPublicProfile(userId),
    enabled: valid,
  });
  const betsQuery = useQuery({
    queryKey: ["user-bets", userId],
    queryFn: () => listUserBets(userId),
    enabled: valid,
  });

  return (
    <section style={{ maxWidth: 720, margin: "0 auto" }}>
      <Link
        to="/leaderboard"
        style={{ fontSize: 13, fontWeight: 600, color: "var(--text2)" }}
      >
        ← Лидерборд
      </Link>

      <div style={{ marginTop: 14 }}>
        {!valid && <ErrorState message="Неверный идентификатор пользователя" />}
        {valid && profileQuery.isPending && <LoadingState />}
        {valid && profileQuery.isError && (
          <ErrorState
            message="Не удалось загрузить профиль"
            onRetry={() => profileQuery.refetch()}
          />
        )}
        {profileQuery.isSuccess && (
          <>
            <ProfileCard profile={profileQuery.data} />

            <div style={{ fontSize: 14, fontWeight: 800, margin: "0 0 10px" }}>
              История ставок
            </div>
            {betsQuery.isPending && <LoadingState />}
            {betsQuery.isError && (
              <ErrorState onRetry={() => betsQuery.refetch()} />
            )}
            {betsQuery.isSuccess && betsQuery.data.items.length === 0 && (
              <EmptyState>У игрока пока нет ставок</EmptyState>
            )}
            {betsQuery.isSuccess && betsQuery.data.items.length > 0 && (
              <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
                {betsQuery.data.items.map((b) => (
                  <BetRow key={b.id} bet={b} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}

function ProfileCard({ profile }: { profile: PublicProfile }) {
  const s = profile.stats;
  const profitColor = s.profit >= 0 ? "var(--green)" : "var(--red)";
  const profitSign = s.profit >= 0 ? "+" : "−";
  const roiSign = s.roi >= 0 ? "+" : "−";

  const cells = [
    { label: "Ставок", value: String(s.total_bets) },
    {
      label: "Винрейт",
      value: s.total_bets ? `${Math.round(s.win_rate * 100)}%` : "—",
    },
    {
      label: "Прибыль",
      value: `${profitSign}${fmtCoins(Math.abs(s.profit))}`,
      color: profitColor,
    },
    {
      label: "ROI",
      value: s.staked ? `${roiSign}${Math.abs(s.roi * 100).toFixed(1)}%` : "—",
      color: profitColor,
    },
  ];

  return (
    <div className="fb-card fb-anim" style={{ padding: 24, borderRadius: 18, marginBottom: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 16, marginBottom: 20 }}>
        {profile.avatar_url ? (
          <img
            src={profile.avatar_url}
            alt={profile.display_name}
            style={{ width: 58, height: 58, borderRadius: "50%", objectFit: "cover", flexShrink: 0 }}
          />
        ) : (
          <span
            style={{
              width: 58,
              height: 58,
              borderRadius: "50%",
              background: avatarBg(profile.display_name),
              color: "#fff",
              fontSize: 20,
              fontWeight: 800,
              display: "grid",
              placeItems: "center",
              flexShrink: 0,
            }}
          >
            {initials(profile.display_name)}
          </span>
        )}
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
            {profile.display_name}
          </div>
          <div style={{ fontSize: 13, color: "var(--text3)", fontWeight: 500, marginTop: 2 }}>
            В игре с {joinedLabel(profile.created_at)}
          </div>
        </div>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(120px,1fr))", gap: 10 }}>
        {cells.map((c) => (
          <div key={c.label} className="fb-stat">
            <div className="fb-stat-label">{c.label}</div>
            <div className="fb-stat-value" style={{ color: c.color }}>{c.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

// BetRow — строка истории. DTO не содержит названий события/исхода/рынка
// (architecture-ограничение M4/M5), поэтому показываем «Событие #id».
function BetRow({ bet }: { bet: Bet }) {
  const meta = BET_STATUS_META[bet.status];
  const res = betResultLine(bet);
  return (
    <div
      className="fb-card"
      style={{ display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap", padding: "13px 16px" }}
    >
      <span
        style={{
          fontSize: 11,
          fontWeight: 700,
          padding: "5px 10px",
          borderRadius: 999,
          background: meta.bg,
          color: meta.color,
          flexShrink: 0,
        }}
      >
        {meta.label}
      </span>
      <div style={{ flex: 1, minWidth: 170 }}>
        <div style={{ fontSize: 14, fontWeight: 700, letterSpacing: "-0.01em" }}>
          Событие #{bet.event_id}
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text2)", marginTop: 2 }}>
          Исход #{bet.outcome_id} <span style={{ color: "var(--text3)" }}>· кф {fmtOdds(bet.odds)}</span>
        </div>
      </div>
      <div style={{ textAlign: "right" }}>
        <div style={{ fontSize: 13.5, fontWeight: 800, color: res.color }}>{res.text}</div>
        <div style={{ fontSize: 11.5, color: "var(--text3)", marginTop: 2 }}>
          Ставка {fmtCoins(bet.stake)} · {absTime(bet.created_at)}
        </div>
      </div>
    </div>
  );
}

// joinedLabel — «месяц год» регистрации (например, «июнь 2026»).
function joinedLabel(iso: string): string {
  return new Date(iso).toLocaleString("ru-RU", { month: "long", year: "numeric" });
}
