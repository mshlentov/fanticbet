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

// BetHandler — HTTP-слой размещения ставок и истории. POST /bets за
// middleware.AuthRequired (user_id в контексте), GET /me/bets — тоже.
type BetHandler struct {
	bets *service.BettingService
}

func NewBetHandler(bets *service.BettingService) *BetHandler {
	return &BetHandler{bets: bets}
}

// --- DTO ---

// placeBetRequest — тело POST /bets. Отдельная структура, не domain.Bet:
// клиент присылает только outcome_id и stake, остальное фиксирует сервис
// (odds, payout, status). Stake > 0 обеспечивает binding:min=1, верхний
// предел и достаточно ли баланса проверяет сервис (ему виднее из конфига).
type placeBetRequest struct {
	OutcomeID int64 `json:"outcome_id" binding:"required,min=1"`
	Stake     int64 `json:"stake" binding:"required,min=1"`
}

// betDTO — ставка в ответе. Odds — decimal, чтобы не терять точность NUMERIC(8,3)
// при сериализации; SettledAt — *time.Time (NULL, пока ставка pending).
type betDTO struct {
	ID              int64           `json:"id"`
	OutcomeID       int64           `json:"outcome_id"`
	EventID         int64           `json:"event_id"`
	Stake           int64           `json:"stake"`
	Odds            decimal.Decimal `json:"odds"`
	PotentialPayout int64           `json:"potential_payout"`
	Status          string          `json:"status"`
	SettledAt       *time.Time      `json:"settled_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

// placeBetResponse — результат размещения: ставка и баланс после списания.
type placeBetResponse struct {
	Bet     betDTO `json:"bet"`
	Balance int64  `json:"balance"`
}

// betsResponse — страница истории ставок: номер страницы + элементы.
type betsResponse struct {
	Page  int      `json:"page"`
	Items []betDTO `json:"items"`
}

// Place — разместить ставку на исход.
//
// @Summary      Разместить ставку
// @Description  Создаёт ставку на исход: проверяет рынок/событие, блокирует кошелёк (FOR UPDATE), списывает фантики и фиксирует коэффициент в одной транзакции.
// @Tags         bets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      placeBetRequest  true  "Параметры ставки"
// @Success      201   {object}  placeBetResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      409   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /bets [post]
func (h *BetHandler) Place(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	var req placeBetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	result, err := h.bets.PlaceBet(c.Request.Context(), userID, req.OutcomeID, req.Stake)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusCreated, placeBetResponse{
		Bet:     toBetDTO(result.Bet),
		Balance: result.Balance,
	})
}

// List — мои ставки (GET /me/bets). Опциональный фильтр ?status= и пагинация ?page=.
//
// @Summary      Мои ставки
// @Description  Страница ставок текущего пользователя (новые — первыми), размер страницы 50. Фильтр по статусу опционален.
// @Tags         bets
// @Produce      json
// @Security     BearerAuth
// @Param        status  query     string  false  "Фильтр по статусу"  Enums(pending, won, lost, void)
// @Param        page    query     int     false  "Номер страницы (с 1)"  default(1)
// @Success      200     {object}  betsResponse
// @Failure      400     {object}  errorResponse
// @Failure      401     {object}  errorResponse
// @Failure      500     {object}  errorResponse
// @Router       /me/bets [get]
func (h *BetHandler) List(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	status := domain.BetStatus(c.Query("status"))
	if status != "" && !isValidBetStatus(status) {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный статус ставки")
		return
	}

	page := 1
	if raw := c.Query("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}

	bets, err := h.bets.ListBets(c.Request.Context(), userID, status, page)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// nil-срез отдаём как пустой массив, чтобы клиент всегда получал [], а не null.
	items := make([]betDTO, 0, len(bets))
	for _, b := range bets {
		items = append(items, toBetDTO(b))
	}
	respondJSON(c, http.StatusOK, betsResponse{Page: page, Items: items})
}

// isValidBetStatus проверяет, что строка — один из известных статусов ставки.
func isValidBetStatus(status domain.BetStatus) bool {
	switch status {
	case domain.BetPending, domain.BetWon, domain.BetLost, domain.BetVoid:
		return true
	}
	return false
}

func toBetDTO(b domain.Bet) betDTO {
	return betDTO{
		ID:              b.ID,
		OutcomeID:       b.OutcomeID,
		EventID:         b.EventID,
		Stake:           b.Stake,
		Odds:            b.Odds,
		PotentialPayout: b.PotentialPayout,
		Status:          string(b.Status),
		SettledAt:       b.SettledAt,
		CreatedAt:       b.CreatedAt,
	}
}
