import { api } from "./client";
import type { Event, EventsPage, SportsResponse } from "./types";

export type EventsFilter = {
  sport?: string;
  status?: string;
  q?: string;
  page?: number;
};

// listSports — виды спорта для фильтра ленты (публичный запрос).
export function listSports(): Promise<SportsResponse> {
  return api.get<SportsResponse>("/sports", { auth: false });
}

// listEvents — лента событий с рынками и текущими коэффициентами.
export function listEvents(filter: EventsFilter = {}): Promise<EventsPage> {
  const params = new URLSearchParams();
  if (filter.sport) params.set("sport", filter.sport);
  if (filter.status) params.set("status", filter.status);
  if (filter.q) params.set("q", filter.q);
  if (filter.page) params.set("page", String(filter.page));

  const qs = params.toString();
  return api.get<EventsPage>(`/events${qs ? `?${qs}` : ""}`, { auth: false });
}

// getEvent — событие со всеми рынками и исходами.
export function getEvent(id: number): Promise<Event> {
  return api.get<Event>(`/events/${id}`, { auth: false });
}
