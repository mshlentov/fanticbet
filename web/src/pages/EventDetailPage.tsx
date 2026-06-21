import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { getEvent } from "../api/events";
import { listMyBets } from "../api/bets";
import type { Bet } from "../api/types";
import { useAuth } from "../hooks/useAuth";
import { OutcomeButton } from "../components/OutcomeButton";
import { StatusBadge } from "./EventsPage";
import { ErrorState, LoadingState } from "../components/states";
import { marketTitle, sportLabel } from "../lib/labels";
import { absTime, fmtOdds } from "../lib/format";
import { BET_STATUS_META, betResultLine } from "../lib/bet";

// EventDetailPage — событие со всеми рынками + блок «мои ставки на это событие».
export function EventDetailPage() {
  const { id } = useParams<{ id: string }>();
  const eventId = Number(id);
  const { status: authStatus } = useAuth();

  const eventQuery = useQuery({
    queryKey: ["event", eventId],
    queryFn: () => getEvent(eventId),
    enabled: Number.isFinite(eventId) && eventId > 0,
  });

  // Мои ставки на это событие — только для авторизованного.
  const myBetsQuery = useQuery({
    queryKey: ["my-bets"],
    queryFn: () => listMyBets(),
    enabled: authStatus === "authenticated",
  });

  if (eventQuery.isPending) return <LoadingState />;
  if (eventQuery.isError) return <ErrorState onRetry={() => eventQuery.refetch()} />;

  const event = eventQuery.data;
  const isCustom = event.source === "custom" || !event.home || !event.away;
  const closed = event.status !== "upcoming";
  const myBets = (myBetsQuery.data?.items ?? []).filter((b) => b.event_id === event.id);

  // Карта outcome_id → подпись исхода для блока «мои ставки».
  const outcomeLabels = new Map<number, string>();
  for (const m of event.markets) {
    for (const o of m.outcomes) outcomeLabels.set(o.id, o.label);
  }

  return (
    <section style={{ maxWidth: 760, margin: "0 auto" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          marginBottom: 14,
          fontSize: 13,
          color: "var(--text3)",
        }}
      >
        <Link to="/" style={{ fontWeight: 600, color: "var(--text2)" }}>
          ← Лента событий
        </Link>
        <span>/</span>
        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {event.title}
        </span>
      </div>

      {/* Шапка события */}
      <div className="fb-card fb-anim" style={{ padding: 22, borderRadius: 18, marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 16 }}>
          <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text2)", flex: 1 }}>
            {event.league_name ?? sportLabel(event.sport_slug)}
          </span>
          <StatusBadge status={event.status} startsAt={event.starts_at} />
        </div>

        {isCustom ? (
          <div style={{ fontSize: 20, fontWeight: 800, letterSpacing: "-0.02em", lineHeight: 1.35 }}>
            {event.title}
          </div>
        ) : (
          <div style={{ display: "grid", gridTemplateColumns: "1fr auto 1fr", alignItems: "center", gap: 14 }}>
            <div style={{ textAlign: "right", fontSize: 19, fontWeight: 800, letterSpacing: "-0.02em" }}>
              {event.home}
            </div>
            <div style={{ fontSize: 27, fontWeight: 800, letterSpacing: "-0.02em", color: "var(--accent)" }}>
              {event.status === "upcoming" ? "VS" : "—"}
            </div>
            <div style={{ fontSize: 19, fontWeight: 800, letterSpacing: "-0.02em" }}>{event.away}</div>
          </div>
        )}

        <div style={{ textAlign: "center", marginTop: 14, fontSize: 12.5, color: "var(--text3)", fontWeight: 500 }}>
          {absTime(event.starts_at)}
        </div>
      </div>

      {/* Рынки */}
      {event.markets.map((m) => (
        <div key={m.id} className="fb-card fb-anim" style={{ padding: 18, marginBottom: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 11, color: "var(--text2)" }}>
            {marketTitle(m)}
            {m.status !== "open" && (
              <span style={{ marginLeft: 8, fontSize: 11, color: "var(--amber)" }}>
                {m.status === "suspended" ? "приостановлен" : m.status}
              </span>
            )}
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {m.outcomes.map((o) => (
              <OutcomeButton
                key={o.id}
                variant="row"
                outcomeId={o.id}
                label={o.label}
                odds={o.odds}
                eventId={event.id}
                eventTitle={event.title}
                marketLabel={marketTitle(m)}
                closed={closed || m.status !== "open"}
              />
            ))}
          </div>
        </div>
      ))}

      {/* Мои ставки на это событие */}
      {authStatus === "authenticated" && (
        <div className="fb-card fb-anim" style={{ padding: 18 }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 11, color: "var(--text2)" }}>
            Мои ставки на это событие
          </div>
          {myBets.length === 0 ? (
            <div style={{ fontSize: 13.5, color: "var(--text3)" }}>
              Вы ещё не делали ставок на это событие.
            </div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {myBets.map((b) => (
                <MyEventBetRow key={b.id} bet={b} outcomeLabel={outcomeLabels.get(b.outcome_id)} />
              ))}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function MyEventBetRow({ bet, outcomeLabel }: { bet: Bet; outcomeLabel?: string }) {
  const meta = BET_STATUS_META[bet.status];
  const res = betResultLine(bet);
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: "11px 13px",
        border: "1px solid var(--border)",
        borderRadius: 12,
        background: "var(--bg)",
      }}
    >
      <span
        style={{
          fontSize: 11,
          fontWeight: 700,
          padding: "4px 9px",
          borderRadius: 999,
          background: meta.bg,
          color: meta.color,
          flexShrink: 0,
        }}
      >
        {meta.label}
      </span>
      <span
        style={{
          flex: 1,
          fontSize: 13,
          fontWeight: 600,
          minWidth: 0,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {outcomeLabel ?? `Исход #${bet.outcome_id}`}{" "}
        <span style={{ color: "var(--text3)", fontWeight: 500 }}>· кф {fmtOdds(bet.odds)}</span>
      </span>
      <span style={{ fontSize: 13, fontWeight: 700, color: res.color, whiteSpace: "nowrap" }}>
        {res.text}
      </span>
    </div>
  );
}
