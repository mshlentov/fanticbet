import { api } from "./client";
import type { Event, EventsPage, LeaguesResponse, SportsResponse } from "./types";

export type EventsFilter = {
  sport?: string;
  source?: string;
  status?: string;
  q?: string;
  league_id?: number;
  page?: number;
};

// listSports — виды спорта для фильтра ленты (публичный запрос).
export function listSports(): Promise<SportsResponse> {
  return api.get<SportsResponse>("/sports", { auth: false });
}

// listLeagues — публичный каталог чемпионатов для фильтра ленты (GET /leagues).
// sportSlug опционален: пустой — все лиги.
export function listLeagues(sportSlug?: string): Promise<LeaguesResponse> {
  const qs = sportSlug ? `?sport_slug=${encodeURIComponent(sportSlug)}` : "";
  return api.get<LeaguesResponse>(`/leagues${qs}`, { auth: false });
}

// listEvents — лента событий с рынками и текущими коэффициентами.
export function listEvents(filter: EventsFilter = {}): Promise<EventsPage> {
  const params = new URLSearchParams();
  if (filter.sport) params.set("sport", filter.sport);
  if (filter.source) params.set("source", filter.source);
  if (filter.status) params.set("status", filter.status);
  if (filter.q) params.set("q", filter.q);
  if (filter.league_id) params.set("league_id", String(filter.league_id));
  if (filter.page) params.set("page", String(filter.page));

  const qs = params.toString();
  return api.get<EventsPage>(`/events${qs ? `?${qs}` : ""}`, { auth: false });
}

// getEvent — событие со всеми рынками и исходами.
export function getEvent(id: number): Promise<Event> {
  return api.get<Event>(`/events/${id}`, { auth: false });
}
