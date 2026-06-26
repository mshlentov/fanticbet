package handler

import (
	"net/http"
	"strconv"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// EventHandler — публичный (без авторизации) HTTP-слой каталога событий:
// список видов спорта, лента событий и одно событие с рынками/коэффициентами.
type EventHandler struct {
	events *service.EventService
}

func NewEventHandler(events *service.EventService) *EventHandler {
	return &EventHandler{events: events}
}

// --- DTO ---

// sportsResponse — список видов спорта для фильтра ленты.
type sportsResponse struct {
	Sports []string `json:"sports"`
}

// outcomeDTO — исход рынка с текущим коэффициентом. odds и result — строки/NULL,
// чтобы не терять точность NUMERIC при сериализации.
type outcomeDTO struct {
	ID     int64           `json:"id"`
	Code   string          `json:"code"`
	Label  string          `json:"label"`
	Odds   decimal.Decimal `json:"odds"`
	Result *string         `json:"result"`
}

// marketDTO — рынок события вместе с исходами.
type marketDTO struct {
	ID       int64            `json:"id"`
	Type     string           `json:"type"`
	Line     *decimal.Decimal `json:"line"`
	Question *string          `json:"question"`
	Status   string           `json:"status"`
	Outcomes []outcomeDTO     `json:"outcomes"`
}

// eventDTO — событие с рынками. Scores в ленте/карточке не отдаём (это сырьё для
// аудита расчёта), поэтому в DTO его нет.
type eventDTO struct {
	ID         int64       `json:"id"`
	Source     string      `json:"source"`
	SportSlug  string      `json:"sport_slug"`
	LeagueName *string     `json:"league_name"`
	Title      string      `json:"title"`
	Home       *string     `json:"home"`
	Away       *string     `json:"away"`
	StartsAt   time.Time   `json:"starts_at"`
	Status     string      `json:"status"`
	Markets    []marketDTO `json:"markets"`
}

// eventsResponse — страница ленты событий.
type eventsResponse struct {
	Page  int        `json:"page"`
	Items []eventDTO `json:"items"`
}

// --- DTO чемпионатов (лиг, M8) ---

// leagueDTO — чемпионат в публичном каталоге. Объявлен локально (а не
// переиспользуется из admin.go), чтобы публичный слой каталога не сцеплялся с
// приватными типами AdminHandler — аналогично outcomeDTO vs adminOutcomeDTO.
type leagueDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SportSlug string    `json:"sport_slug"`
}

// leaguesResponse — список чемпионатов для фильтра ленты.
type leaguesResponse struct {
	Items []leagueDTO `json:"items"`
}

// Sports — список видов спорта, по которым есть события (+ custom).
//
// @Summary      Виды спорта
// @Description  Список видов спорта, по которым есть события в БД (всегда включает custom).
// @Tags         events
// @Produce      json
// @Success      200  {object}  sportsResponse
// @Failure      500  {object}  errorResponse
// @Router       /sports [get]
func (h *EventHandler) Sports(c *gin.Context) {
	sports, err := h.events.ListSports(c.Request.Context())
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}
	if sports == nil {
		sports = []string{}
	}
	respondJSON(c, http.StatusOK, sportsResponse{Sports: sports})
}

// List — лента событий с рынками и текущими коэффициентами.
//
// @Summary      Лента событий
// @Description  Страница событий с рынками и текущими коэффициентами. Фильтры sport/status/league_id/q опциональны.
// @Tags         events
// @Produce      json
// @Param        sport      query     string  false  "Фильтр по виду спорта (sport_slug)"
// @Param        status     query     string  false  "Фильтр по статусу"  Enums(upcoming, live, settled, cancelled)
// @Param        league_id  query     int     false  "Фильтр по чемпионату (id)"
// @Param        q          query     string  false  "Поиск по названию события"
// @Param        page       query     int     false  "Номер страницы (с 1)"  default(1)
// @Success      200     {object}  eventsResponse
// @Failure      400     {object}  errorResponse
// @Failure      500     {object}  errorResponse
// @Router       /events [get]
func (h *EventHandler) List(c *gin.Context) {
	status := c.Query("status")
	if status != "" && !isValidEventStatus(status) {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный статус события")
		return
	}

	page := 1
	if raw := c.Query("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}

	// league_id опционален: nil, если параметр пуст/невалиден (но не 0/негатив).
	var leagueID *int64
	if raw := c.Query("league_id"); raw != "" {
		lid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || lid <= 0 {
			respondError(c, http.StatusBadRequest, "validation_error", "Неверный идентификатор чемпионата")
			return
		}
		leagueID = &lid
	}

	filter := repository.EventFilter{
		Sport:    c.Query("sport"),
		Status:   domain.EventStatus(status),
		LeagueID: leagueID,
		Query:    c.Query("q"),
		Page:     page,
	}

	events, err := h.events.ListEvents(c.Request.Context(), filter)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	items := make([]eventDTO, 0, len(events))
	for _, e := range events {
		items = append(items, toEventDTO(e))
	}
	respondJSON(c, http.StatusOK, eventsResponse{Page: page, Items: items})
}

// Get — одно событие со всеми рынками и исходами.
//
// @Summary      Событие
// @Description  Событие со всеми рынками и исходами (текущие коэффициенты).
// @Tags         events
// @Produce      json
// @Param        id   path      int  true  "ID события"
// @Success      200  {object}  eventDTO
// @Failure      400  {object}  errorResponse
// @Failure      404  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /events/{id} [get]
func (h *EventHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный идентификатор события")
		return
	}

	event, err := h.events.GetEvent(c.Request.Context(), id)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}
	respondJSON(c, http.StatusOK, toEventDTO(event))
}

// isValidEventStatus проверяет, что строка — один из известных статусов события.
func isValidEventStatus(status string) bool {
	switch domain.EventStatus(status) {
	case domain.EventUpcoming, domain.EventLive, domain.EventSettled, domain.EventCancelled:
		return true
	}
	return false
}

func toEventDTO(e service.EventWithMarkets) eventDTO {
	markets := make([]marketDTO, 0, len(e.Markets))
	for _, m := range e.Markets {
		markets = append(markets, toMarketDTO(m))
	}
	return eventDTO{
		ID:         e.Event.ID,
		Source:     string(e.Event.Source),
		SportSlug:  e.Event.SportSlug,
		LeagueName: e.Event.LeagueName,
		Title:      e.Event.Title,
		Home:       e.Event.Home,
		Away:       e.Event.Away,
		StartsAt:   e.Event.StartsAt,
		Status:     string(e.Event.Status),
		Markets:    markets,
	}
}

func toMarketDTO(m service.MarketWithOutcomes) marketDTO {
	outcomes := make([]outcomeDTO, 0, len(m.Outcomes))
	for _, o := range m.Outcomes {
		outcomes = append(outcomes, toOutcomeDTO(o))
	}
	return marketDTO{
		ID:       m.Market.ID,
		Type:     string(m.Market.Type),
		Line:     m.Market.Line,
		Question: m.Market.Question,
		Status:   string(m.Market.Status),
		Outcomes: outcomes,
	}
}

func toOutcomeDTO(o domain.Outcome) outcomeDTO {
	var result *string
	if o.Result != nil {
		s := string(*o.Result)
		result = &s
	}
	return outcomeDTO{
		ID:     o.ID,
		Code:   string(o.Code),
		Label:  o.Label,
		Odds:   o.Odds,
		Result: result,
	}
}

// ListLeagues — публичный список чемпионатов с опциональным фильтром по sport_slug
// (GET /leagues?sport_slug=). Используется фильтром ленты событий.
//
// @Summary      Чемпионаты
// @Description  Список чемпионатов (лиг) для фильтра ленты. Параметр sport_slug опционален.
// @Tags         events
// @Produce      json
// @Param        sport_slug  query     string  false  "Фильтр по виду спорта (sport_slug)"
// @Success      200  {object}  leaguesResponse
// @Failure      500  {object}  errorResponse
// @Router       /leagues [get]
func (h *EventHandler) ListLeagues(c *gin.Context) {
	leagues, err := h.events.ListLeagues(c.Request.Context(), c.Query("sport_slug"))
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	items := make([]leagueDTO, 0, len(leagues))
	for _, l := range leagues {
		items = append(items, leagueDTO{
			ID:        l.ID,
			Name:      l.Name,
			SportSlug: l.SportSlug,
		})
	}
	respondJSON(c, http.StatusOK, leaguesResponse{Items: items})
}
