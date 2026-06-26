package handler

import (
	"net/http"
	"strconv"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/handler/middleware"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// AdminHandler — HTTP-слой админки. Все маршруты за middleware.AuthRequired +
// middleware.AdminRequired (роль admin), поэтому admin_id всегда в контексте.
// Хендлеры не содержат бизнес-логики: парсинг DTO → вызов AdminService → маппинг
// ответа. Ошибки сервиса мапятся на HTTP через mapDomainErr.
type AdminHandler struct {
	admin *service.AdminService
}

func NewAdminHandler(admin *service.AdminService) *AdminHandler {
	return &AdminHandler{admin: admin}
}

// --- DTO создания кастомного события ---

// createOutcomeDTO — исход при создании. Odds — строка: NUMERIC(8,3) сериализуется
// как строка, чтобы не терять точность (как в outcomeDTO.Odds).
type createOutcomeDTO struct {
	Label string `json:"label" binding:"required"`
	Odds  string `json:"odds" binding:"required"`
}

// createMarketDTO — рынок при создании: вопрос + исходы.
type createMarketDTO struct {
	Question *string            `json:"question"`
	Outcomes []createOutcomeDTO `json:"outcomes" binding:"required,dive"`
}

// createEventRequest — тело POST /admin/events.
type createEventRequest struct {
	Title    string          `json:"title" binding:"required"`
	StartsAt time.Time       `json:"starts_at" binding:"required"`
	Market   createMarketDTO `json:"market" binding:"required"`
}

// adminOutcomeDTO — исход в ответе admin-эндпоинтов. Объявлен локально (а не
// переиспользуется из event.go), чтобы admin-слой был самодостаточен и не
// сцеплялся с приватными типами EventHandler.
type adminOutcomeDTO struct {
	ID    int64           `json:"id"`
	Code  string          `json:"code"`
	Label string          `json:"label"`
	Odds  decimal.Decimal `json:"odds"`
}

// adminMarketDTO — рынок в ответе.
type adminMarketDTO struct {
	ID       int64            `json:"id"`
	Type     string           `json:"type"`
	Question *string          `json:"question"`
	Status   string           `json:"status"`
	Outcomes []adminOutcomeDTO `json:"outcomes"`
}

// createEventResponse — результат создания: событие с рынком и исходами.
type createEventResponse struct {
	ID       int64           `json:"id"`
	Source   string          `json:"source"`
	Title    string          `json:"title"`
	StartsAt time.Time       `json:"starts_at"`
	Status   string          `json:"status"`
	Market   adminMarketDTO  `json:"market"`
}

// --- DTO правки события ---

// editOutcomeDTO — правка одного исхода по id. Label/Odds опциональны.
type editOutcomeDTO struct {
	ID    int64  `json:"id" binding:"required,min=1"`
	Label *string `json:"label"`
	Odds  *string `json:"odds"`
}

// editEventRequest — тело PATCH /admin/events/:id. Все поля опциональны, кроме
// status (если задан — должен быть "cancelled", что выполняет отмену).
type editEventRequest struct {
	Title    *string          `json:"title"`
	StartsAt *time.Time       `json:"starts_at"`
	Question *string          `json:"question"`
	Status   *string          `json:"status"` // единственное допустимое значение: "cancelled"
	Outcomes []editOutcomeDTO `json:"outcomes"`
}

// --- DTO расчёта и корректировки ---

// settleRequest — тело POST /admin/events/:id/settle: id победившего исхода.
type settleRequest struct {
	WinningOutcomeID int64 `json:"winning_outcome_id" binding:"required,min=1"`
}

// adjustRequest — тело POST /admin/users/:id/adjust. amount ≠ 0 (валидация в
// сервисе); reason обязателен и попадёт в лог сервера.
type adjustRequest struct {
	Amount int64  `json:"amount" binding:"required"`
	Reason string `json:"reason" binding:"required"`
}

// adjustResponse — результат корректировки: новый баланс.
type adjustResponse struct {
	Balance int64 `json:"balance"`
}

// --- DTO чемпионатов (лиг, M8) ---

// adminLeagueDTO — чемпионат в ответе admin-эндпоинтов. Объявлен локально (а не
// переиспользуется из event.go), чтобы admin-слой был самодостаточен — аналогично
// adminOutcomeDTO vs outcomeDTO.
type adminLeagueDTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SportSlug string    `json:"sport_slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// adminLeaguesResponse — страница списка чемпионатов админки.
type adminLeaguesResponse struct {
	Items []adminLeagueDTO `json:"items"`
}

// createLeagueRequest — тело POST /admin/leagues. Поля обязательны.
type createLeagueRequest struct {
	Name      string `json:"name" binding:"required"`
	SportSlug string `json:"sport_slug" binding:"required"`
}

// editLeagueRequest — тело PATCH /admin/leagues/:id. Оба поля опциональны.
type editLeagueRequest struct {
	Name      *string `json:"name"`
	SportSlug *string `json:"sport_slug"`
}

// --- Хендлеры ---

// CreateEvent — создать кастомное событие (POST /admin/events).
//
// @Summary      Создать кастомное событие
// @Description  Создаёт кастомное событие (source='custom') с одним CUSTOM-рынком и исходами в одной транзакции.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createEventRequest  true  "Параметры события"
// @Success      201   {object}  createEventResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/events [post]
func (h *AdminHandler) CreateEvent(c *gin.Context) {
	adminID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	var req createEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	// Парсим коэффициенты исходов из строк в decimal. Невалидный odds → 400.
	outcomes := make([]service.CustomOutcomeInput, 0, len(req.Market.Outcomes))
	for i, oc := range req.Market.Outcomes {
		odds, err := decimal.NewFromString(oc.Odds)
		if err != nil {
			respondError(c, http.StatusBadRequest, "validation_error",
				"Неверный формат коэффициента в исходе "+strconv.Itoa(i))
			return
		}
		label := oc.Label
		outcomes = append(outcomes, service.CustomOutcomeInput{
			Label: &label,
			Odds:  &odds,
		})
	}

	created, err := h.admin.CreateCustomEvent(c.Request.Context(), adminID, service.CustomEventInput{
		Title:    req.Title,
		StartsAt: req.StartsAt,
		Market: service.CustomMarketInput{
			Question: req.Market.Question,
			Outcomes: outcomes,
		},
	})
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusCreated, toCreateEventResponse(created))
}

// EditEvent — правка/отмена кастомного события (PATCH /admin/events/:id).
//
// @Summary      Редактировать кастомное событие
// @Description  Правит title/starts_at/question/коэффициенты исходов или отменяет событие (status='cancelled' → void ставок). Только для source='custom', status='upcoming'.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int                true  "ID события"
// @Param        body  body      editEventRequest   true  "Поля для обновления"
// @Success      204   {object}  nil
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      409   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/events/{id} [patch]
func (h *AdminHandler) EditEvent(c *gin.Context) {
	eventID, ok := parseAdminID(c, "id")
	if !ok {
		return // parseAdminID уже отправил ответ
	}

	var req editEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	// status отличен от nil и не "cancelled" — это единственное поддерживаемое
	// действие через status. Иначе — 400, чтобы не плодить полу-реализованные
	// переходы (live/settled админ не выставляет вручную).
	cancel := false
	if req.Status != nil {
		if domain.EventStatus(*req.Status) != domain.EventCancelled {
			respondError(c, http.StatusBadRequest, "validation_error",
				"Единственное допустимое значение status: cancelled")
			return
		}
		cancel = true
	}

	// Парсим коэффициенты правки из строк.
	outcomes := make([]service.EditOutcomeInput, 0, len(req.Outcomes))
	for i, oc := range req.Outcomes {
		var oddsPtr *decimal.Decimal
		if oc.Odds != nil {
			odds, err := decimal.NewFromString(*oc.Odds)
			if err != nil {
				respondError(c, http.StatusBadRequest, "validation_error",
					"Неверный формат коэффициента в исходе "+strconv.Itoa(i))
				return
			}
			oddsPtr = &odds
		}
		outcomes = append(outcomes, service.EditOutcomeInput{
			ID:    oc.ID,
			Label: oc.Label,
			Odds:  oddsPtr,
		})
	}

	if err := h.admin.EditEvent(c.Request.Context(), eventID, service.EditEventInput{
		Title:    req.Title,
		StartsAt: req.StartsAt,
		Question: req.Question,
		Cancel:   cancel,
		Outcomes: outcomes,
	}); err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusNoContent, nil)
}

// SettleEvent — ручной расчёт кастомного события (POST /admin/events/:id/settle).
//
// @Summary      Рассчитать кастомное событие
// @Description  Рассчитывает кастомное событие по выбранному победившему исходу: выигрыш — выплата, проигрыш — списание, рынок → settled.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int             true  "ID события"
// @Param        body  body      settleRequest   true  "ID победившего исхода"
// @Success      204   {object}  nil
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      409   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/events/{id}/settle [post]
func (h *AdminHandler) SettleEvent(c *gin.Context) {
	eventID, ok := parseAdminID(c, "id")
	if !ok {
		return
	}

	var req settleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	if err := h.admin.SettleCustom(c.Request.Context(), eventID, req.WinningOutcomeID); err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusNoContent, nil)
}

// AdjustBalance — ручная корректировка баланса пользователя (POST /admin/users/:id/adjust).
//
// @Summary      Скорректировать баланс
// @Description  Меняет баланс пользователя на amount (может быть отрицательным) в транзакции с FOR UPDATE. reason пишется в лог сервера.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int              true  "ID пользователя"
// @Param        body  body      adjustRequest    true  "Сумма и причина"
// @Success      200   {object}  adjustResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/users/{id}/adjust [post]
func (h *AdminHandler) AdjustBalance(c *gin.Context) {
	userID, ok := parseAdminID(c, "id")
	if !ok {
		return
	}

	var req adjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	balance, err := h.admin.AdjustBalance(c.Request.Context(), userID, req.Amount, req.Reason)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusOK, adjustResponse{Balance: balance})
}

// --- Хендлеры чемпионатов (лиг, M8) ---

// ListLeagues — список чемпионатов с опциональным фильтром по sport_slug
// (GET /admin/leagues?sport_slug=).
//
// @Summary      Список чемпионатов
// @Description  Список чемпионатов (лиг). Параметр sport_slug опционален.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        sport_slug  query     string  false  "Фильтр по виду спорта (sport_slug)"
// @Success      200  {object}  adminLeaguesResponse
// @Failure      401  {object}  errorResponse
// @Failure      403  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /admin/leagues [get]
func (h *AdminHandler) ListLeagues(c *gin.Context) {
	leagues, err := h.admin.ListLeagues(c.Request.Context(), c.Query("sport_slug"))
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusOK, adminLeaguesResponse{Items: toAdminLeagueDTOs(leagues)})
}

// CreateLeague — создать чемпионат (POST /admin/leagues).
//
// @Summary      Создать чемпионат
// @Description  Создаёт чемпионат (лигу): {name, sport_slug}. Дубликаты (name, sport_slug) допустимы — различаются по id.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createLeagueRequest  true  "Параметры чемпионата"
// @Success      201   {object}  adminLeagueDTO
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/leagues [post]
func (h *AdminHandler) CreateLeague(c *gin.Context) {
	_, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	var req createLeagueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	league, err := h.admin.CreateLeague(c.Request.Context(), service.CreateLeagueInput{
		Name:      req.Name,
		SportSlug: req.SportSlug,
	})
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusCreated, toAdminLeagueDTO(league))
}

// EditLeague — переименовать / сменить спорт чемпионата (PATCH /admin/leagues/:id).
//
// @Summary      Редактировать чемпионат
// @Description  Правит name и/или sport_slug чемпионата. Оба поля опциональны.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int                 true  "ID чемпионата"
// @Param        body  body      editLeagueRequest   true  "Поля для обновления"
// @Success      204   {object}  nil
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /admin/leagues/{id} [patch]
func (h *AdminHandler) EditLeague(c *gin.Context) {
	id, ok := parseAdminID(c, "id")
	if !ok {
		return // parseAdminID уже отправил ответ
	}

	var req editLeagueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	if err := h.admin.UpdateLeague(c.Request.Context(), id, service.UpdateLeagueInput{
		Name:      req.Name,
		SportSlug: req.SportSlug,
	}); err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusNoContent, nil)
}

// DeleteLeague — удалить чемпионат (DELETE /admin/leagues/:id). Запрещено, если
// к чемпионату привязаны события (409 conflict).
//
// @Summary      Удалить чемпионат
// @Description  Удаляет чемпионат. Если к нему привязаны события — 409 conflict.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int  true  "ID чемпионата"
// @Success      204   {object}  nil
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      409   {object}  errorResponse
// @Router       /admin/leagues/{id} [delete]
func (h *AdminHandler) DeleteLeague(c *gin.Context) {
	id, ok := parseAdminID(c, "id")
	if !ok {
		return // parseAdminID уже отправил ответ
	}

	if err := h.admin.DeleteLeague(c.Request.Context(), id); err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusNoContent, nil)
}

// toAdminLeagueDTO маппит доменную лигу в DTO ответа админки.
func toAdminLeagueDTO(l domain.League) adminLeagueDTO {
	return adminLeagueDTO{
		ID:        l.ID,
		Name:      l.Name,
		SportSlug: l.SportSlug,
		CreatedAt: l.CreatedAt,
		UpdatedAt: l.UpdatedAt,
	}
}

// toAdminLeagueDTOs маппит срез доменных лиг в DTO. Не забываем про nil → пустой
// срез, чтобы ответ всегда содержал "items": [], а не null.
func toAdminLeagueDTOs(leagues []domain.League) []adminLeagueDTO {
	items := make([]adminLeagueDTO, 0, len(leagues))
	for _, l := range leagues {
		items = append(items, toAdminLeagueDTO(l))
	}
	return items
}

// parseAdminID достаёт и валидирует path-параметр :id. При ошибке отправляет 400
// и возвращает ok=false; в этом случае caller должен сразу вернуть управление.
func parseAdminID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный идентификатор")
		return 0, false
	}
	return id, true
}

// toCreateEventResponse маппит результат создания сервиса в DTO ответа.
func toCreateEventResponse(c service.CreatedEvent) createEventResponse {
	outcomes := make([]adminOutcomeDTO, 0, len(c.Outcomes))
	for _, o := range c.Outcomes {
		outcomes = append(outcomes, adminOutcomeDTO{
			ID:    o.ID,
			Code:  string(o.Code),
			Label: o.Label,
			Odds:  o.Odds,
		})
	}
	return createEventResponse{
		ID:       c.Event.ID,
		Source:   string(c.Event.Source),
		Title:    c.Event.Title,
		StartsAt: c.Event.StartsAt,
		Status:   string(c.Event.Status),
		Market: adminMarketDTO{
			ID:       c.Market.ID,
			Type:     string(c.Market.Type),
			Question: c.Market.Question,
			Status:   string(c.Market.Status),
			Outcomes: outcomes,
		},
	}
}
