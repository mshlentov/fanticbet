import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { listEvents, listLeagues, listSports } from "../api/events";
import type { Event } from "../api/types";
import { OutcomeButton } from "../components/OutcomeButton";
import { SportIcon } from "../components/icons";
import { EmptyState, ErrorState, LoadingState } from "../components/states";
import { eventsCountLabel, marketTitle, sportLabel, statusOrder } from "../lib/labels";
import { absTime, relTime } from "../lib/format";

const STATUS_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "Все" },
  { value: "upcoming", label: "Предстоящие" },
  { value: "live", label: "Live" },
];

// EventsPage — главная лента событий: фильтры по виду спорта и статусу, сетка
// карточек с рынками и кликабельными коэффициентами (добавляются в купон).
export function EventsPage() {
  const [sport, setSport] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [leagueId, setLeagueId] = useState("");

  const sportsQuery = useQuery({ queryKey: ["sports"], queryFn: listSports });
  // Каталог чемпионатов для выбранного вида спорта (для фильтра ленты).
  const leaguesQuery = useQuery({
    queryKey: ["leagues", sport],
    queryFn: () => listLeagues(sport || undefined),
  });
  const eventsQuery = useQuery({
    queryKey: ["events", { sport, status: statusFilter, leagueId }],
    queryFn: () =>
      listEvents({
        sport: sport || undefined,
        status: statusFilter || undefined,
        league_id: leagueId ? Number(leagueId) : undefined,
      }),
  });

  // Смена вида спорта сбрасывает выбранный чемпионат (лиги привязаны к спорту).
  const handleSport = (value: string) => {
    setSport(value);
    setLeagueId("");
  };

  const leagues = leaguesQuery.data?.items ?? [];

  // Сортировка ленты: live → upcoming → settled, внутри — по времени старта.
  const events = useMemo(() => {
    const items = eventsQuery.data?.items ?? [];
    return [...items].sort(
      (a, b) =>
        statusOrder(a.status) - statusOrder(b.status) ||
        new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime(),
    );
  }, [eventsQuery.data]);

  const sportChips = [
    { value: "", label: "Все виды" },
    ...(sportsQuery.data?.sports ?? []).map((s) => ({ value: s, label: sportLabel(s) })),
  ];

  return (
    <section>
      <FeaturedSection />

      <div style={{ display: "flex", alignItems: "baseline", gap: 12, marginBottom: 16 }}>
        <h1 style={{ margin: 0, fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
          Лента событий
        </h1>
        {eventsQuery.isSuccess && (
          <span style={{ fontSize: 13, color: "var(--text3)", fontWeight: 500 }}>
            {eventsCountLabel(events.length)}
          </span>
        )}
      </div>

      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 8,
          alignItems: "center",
          marginBottom: 18,
        }}
      >
        {sportChips.map((c) => (
          <button
            key={c.value || "all"}
            type="button"
            className={`fb-chip${sport === c.value ? " is-active" : ""}`}
            onClick={() => handleSport(c.value)}
          >
            {c.label}
          </button>
        ))}
        <span className="fb-chip-divider" />
        {STATUS_FILTERS.map((c) => (
          <button
            key={c.value || "any"}
            type="button"
            className={`fb-chip${statusFilter === c.value ? " is-active" : ""}`}
            onClick={() => setStatusFilter(c.value)}
          >
            {c.label}
          </button>
        ))}
        {leagues.length > 0 && (
          <>
            <span className="fb-chip-divider" />
            <select
              className="fb-input"
              value={leagueId}
              onChange={(e) => setLeagueId(e.target.value)}
              style={{ height: 34, padding: "0 10px", fontSize: 13, width: "auto" }}
              aria-label="Фильтр по чемпионату"
            >
              <option value="">Все чемпионаты</option>
              {leagues.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
          </>
        )}
      </div>

      {eventsQuery.isPending && <LoadingState />}
      {eventsQuery.isError && <ErrorState onRetry={() => eventsQuery.refetch()} />}
      {eventsQuery.isSuccess && events.length === 0 && (
        <EmptyState>Нет событий по выбранным фильтрам</EmptyState>
      )}

      <div className="fb-events-grid">
        {events.map((ev) => (
          <EventCard key={ev.id} event={ev} />
        ))}
      </div>
    </section>
  );
}

// FeaturedSection — секция «Популярные события» над основной лентой. Отдельный
// запрос (featured=true), своя сетка из тех же EventCard. Скрывается, если
// популярных событий нет (или запрос ещё/не успешен) — чтобы не занимать место.
function FeaturedSection() {
  const featuredQuery = useQuery({
    queryKey: ["events", "featured"],
    queryFn: () => listEvents({ featured: true }),
  });

  const featured = featuredQuery.data?.items ?? [];
  if (featured.length === 0) return null;

  return (
    <div style={{ marginBottom: 28 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 14 }}>
        <span style={{ fontSize: 18 }}>⭐</span>
        <h2 style={{ margin: 0, fontSize: 18, fontWeight: 800, letterSpacing: "-0.02em" }}>
          Популярные события
        </h2>
      </div>
      <div className="fb-events-grid">
        {featured.map((ev) => (
          <EventCard key={ev.id} event={ev} />
        ))}
      </div>
    </div>
  );
}

function EventCard({ event }: { event: Event }) {
  const navigate = useNavigate();
  const isCustom = event.source === "custom" || !event.home || !event.away;
  const closed = event.status !== "upcoming";

  return (
    <div
      className="fb-card fb-event-card fb-anim"
      onClick={() => navigate(`/events/${event.id}`)}
    >
      {/* Шапка карточки: иконка спорта, лига, статус/время */}
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span
          style={{
            width: 26,
            height: 26,
            borderRadius: 8,
            background: "var(--surface2)",
            display: "grid",
            placeItems: "center",
            color: "var(--text2)",
            flexShrink: 0,
          }}
        >
          <SportIcon sport={event.sport_slug} />
        </span>
        <span
          style={{
            fontSize: 12,
            fontWeight: 600,
            color: "var(--text2)",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            flex: 1,
            minWidth: 0,
          }}
        >
          {event.league_name ?? sportLabel(event.sport_slug)}
        </span>
        {event.is_featured && (
          <span title="Популярное" style={{ color: "var(--accent)", fontSize: 13, flexShrink: 0 }}>
            ★
          </span>
        )}
        <StatusBadge status={event.status} startsAt={event.starts_at} />
      </div>

      {/* Заголовок: матч (две строки) или кастомный вопрос */}
      {isCustom ? (
        <div style={{ fontSize: 14.5, fontWeight: 700, lineHeight: 1.4, minHeight: 40 }}>
          {event.title}
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 5 }}>
          <span style={{ fontSize: 15, fontWeight: 700, letterSpacing: "-0.01em" }}>
            {event.home}
          </span>
          <span style={{ fontSize: 15, fontWeight: 700, letterSpacing: "-0.01em" }}>
            {event.away}
          </span>
        </div>
      )}

      {/* Рынки и коэффициенты */}
      <div style={{ display: "flex", flexDirection: "column", gap: 9, marginTop: "auto" }}>
        {event.markets.map((m) => (
          <div key={m.id} style={{ display: "flex", flexDirection: "column", gap: 5 }}>
            <span
              style={{
                fontSize: 10.5,
                fontWeight: 700,
                color: "var(--text3)",
                textTransform: "uppercase",
                letterSpacing: "0.06em",
              }}
            >
              {marketTitle(m)}
            </span>
            <div style={{ display: "flex", gap: 6 }}>
              {m.outcomes.map((o) => (
                <OutcomeButton
                  key={o.id}
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
      </div>
    </div>
  );
}

// StatusBadge — бейдж статуса события (LIVE / Завершено / относительное время).
export function StatusBadge({ status, startsAt }: { status: string; startsAt: string }) {
  if (status === "live") {
    return (
      <span className="fb-badge fb-badge-live">
        <span className="fb-live-dot" />
        LIVE
      </span>
    );
  }
  if (status === "settled" || status === "cancelled") {
    return (
      <span className="fb-badge fb-badge-settled">
        {status === "cancelled" ? "Отменено" : "Завершено"}
      </span>
    );
  }
  return (
    <span style={{ textAlign: "right", lineHeight: 1.25, flexShrink: 0 }}>
      <span style={{ display: "block", fontSize: 12, fontWeight: 700, color: "var(--accent)" }}>
        {relTime(startsAt, status)}
      </span>
      <span style={{ display: "block", fontSize: 11, color: "var(--text3)" }}>
        {absTime(startsAt)}
      </span>
    </span>
  );
}
