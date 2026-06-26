package handler

import (
	"net/http"
	"strconv"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
)

// StatsHandler — публичный (без авторизации) HTTP-слой социальной части M5:
// профиль пользователя со статистикой, публичная история ставок и лидерборд.
// История ставок публична по продукту (architecture.md:197), поэтому эти
// маршруты не требуют AuthRequired.
type StatsHandler struct {
	stats *service.StatsService
}

func NewStatsHandler(stats *service.StatsService) *StatsHandler {
	return &StatsHandler{stats: stats}
}

// --- DTO ---

// userStatsDTO — агрегированная статистика пользователя по ставкам. Все счётчики
// и суммы — целые (int64); WinRate/ROI — доли (0..1+ для ROI при прибыли > stake).
type userStatsDTO struct {
	TotalBets   int64   `json:"total_bets"`
	WonBets     int64   `json:"won_bets"`
	LostBets    int64   `json:"lost_bets"`
	VoidBets    int64   `json:"void_bets"`
	PendingBets int64   `json:"pending_bets"`
	Staked      int64   `json:"staked"`
	Profit      int64   `json:"profit"`
	WinRate     float64 `json:"win_rate"`
	ROI         float64 `json:"roi"`
}

// publicProfileDTO — публичный профиль: только общедоступные поля (email/роль/
// пароль не отдаём) + статистика. По составу уже, чем userDTO из user.go (там
// email и last_login — приватные данные текущего пользователя).
type publicProfileDTO struct {
	ID          int64        `json:"id"`
	DisplayName string       `json:"display_name"`
	AvatarURL   *string      `json:"avatar_url"`
	CreatedAt   time.Time    `json:"created_at"`
	Stats       userStatsDTO `json:"stats"`
}

// leaderboardRowDTO — строка таблицы лидеров: публичное имя/аватар + метрики.
type leaderboardRowDTO struct {
	UserID      int64   `json:"user_id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	TotalBets   int64   `json:"total_bets"`
	WonBets     int64   `json:"won_bets"`
	Staked      int64   `json:"staked"`
	Profit      int64   `json:"profit"`
	ROI         float64 `json:"roi"`
}

// leaderboardResponse — страница лидерборда: номер страницы + элементы.
type leaderboardResponse struct {
	Page  int                 `json:"page"`
	Items []leaderboardRowDTO `json:"items"`
}

// userBetsResponse — страница публичной истории ставок пользователя.
type userBetsResponse struct {
	Page  int      `json:"page"`
	Items []betDTO `json:"items"`
}

// GetUser — публичный профиль пользователя со статистикой (GET /users/:id).
//
// @Summary      Публичный профиль
// @Description  Профиль пользователя и агрегированная статистика по ставкам (всего, winrate, profit, ROI).
// @Tags         users
// @Produce      json
// @Param        id   path      int  true  "ID пользователя"
// @Success      200  {object}  publicProfileDTO
// @Failure      400  {object}  errorResponse
// @Failure      404  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /users/{id} [get]
func (h *StatsHandler) GetUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный идентификатор пользователя")
		return
	}

	profile, err := h.stats.GetPublicProfile(c.Request.Context(), id)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusOK, toPublicProfileDTO(profile))
}

// UserBets — публичная история ставок пользователя (GET /users/:id/bets).
//
// @Summary      Ставки пользователя
// @Description  Страница ставок пользователя (новые — первыми), размер страницы 50. Фильтр по статусу опционален.
// @Tags         users
// @Produce      json
// @Param        id      path      int     true  "ID пользователя"
// @Param        status  query     string  false  "Фильтр по статусу"  Enums(pending, won, lost, void)
// @Param        page    query     int     false  "Номер страницы (с 1)"  default(1)
// @Success      200     {object}  userBetsResponse
// @Failure      400     {object}  errorResponse
// @Failure      500     {object}  errorResponse
// @Router       /users/{id}/bets [get]
func (h *StatsHandler) UserBets(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверный идентификатор пользователя")
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

	bets, err := h.stats.ListUserBets(c.Request.Context(), id, status, page)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// nil-срез отдаём как пустой массив, чтобы клиент всегда получал [], а не null.
	items := make([]betDTO, 0, len(bets))
	for _, b := range bets {
		items = append(items, toBetDTODetailed(b))
	}
	respondJSON(c, http.StatusOK, userBetsResponse{Page: page, Items: items})
}

// Leaderboard — топ прогнозистов (GET /leaderboard). period по умолчанию all,
// metric — profit, page — 1. Результат кэшируется in-memory на 60с.
//
// @Summary      Лидерборд
// @Description  Топ прогнозистов по прибыли или ROI за период. Результат кэшируется на 60 секунд.
// @Tags         leaderboard
// @Produce      json
// @Param        period  query     string  false  "Период"  Enums(week, month, all)  default(all)
// @Param        metric  query     string  false  "Метрика сортировки"  Enums(profit, roi)  default(profit)
// @Param        page    query     int     false  "Номер страницы (с 1)"  default(1)
// @Success      200     {object}  leaderboardResponse
// @Failure      400     {object}  errorResponse
// @Failure      500     {object}  errorResponse
// @Router       /leaderboard [get]
func (h *StatsHandler) Leaderboard(c *gin.Context) {
	// Период: пустой → all (умолчание). Незнакомое значение — 400.
	period := domain.PeriodAll
	if raw := c.Query("period"); raw != "" {
		if !isValidPeriod(domain.StatsPeriod(raw)) {
			respondError(c, http.StatusBadRequest, "validation_error", "Неверный период лидерборда")
			return
		}
		period = domain.StatsPeriod(raw)
	}

	// Метрика: пустой → profit (умолчание). Незнакомое значение — 400.
	metric := domain.MetricProfit
	if raw := c.Query("metric"); raw != "" {
		if !isValidMetric(domain.LeaderboardMetric(raw)) {
			respondError(c, http.StatusBadRequest, "validation_error", "Неверная метрика лидерборда")
			return
		}
		metric = domain.LeaderboardMetric(raw)
	}

	page := 1
	if raw := c.Query("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}

	rows, err := h.stats.GetLeaderboard(c.Request.Context(), period, metric, page)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// nil-срез отдаём как пустой массив, чтобы клиент всегда получал [], а не null.
	items := make([]leaderboardRowDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toLeaderboardRowDTO(r))
	}
	respondJSON(c, http.StatusOK, leaderboardResponse{Page: page, Items: items})
}

// isValidPeriod проверяет, что строка — один из известных периодов лидерборда.
func isValidPeriod(p domain.StatsPeriod) bool {
	switch p {
	case domain.PeriodWeek, domain.PeriodMonth, domain.PeriodAll:
		return true
	}
	return false
}

// isValidMetric проверяет, что строка — одна из известных метрик лидерборда.
func isValidMetric(m domain.LeaderboardMetric) bool {
	switch m {
	case domain.MetricProfit, domain.MetricROI:
		return true
	}
	return false
}

func toPublicProfileDTO(p service.PublicProfile) publicProfileDTO {
	return publicProfileDTO{
		ID:          p.User.ID,
		DisplayName: p.User.DisplayName,
		AvatarURL:   p.User.AvatarURL,
		CreatedAt:   p.User.CreatedAt,
		Stats:       toUserStatsDTO(p.Stats),
	}
}

func toUserStatsDTO(s domain.UserStats) userStatsDTO {
	return userStatsDTO{
		TotalBets:   s.TotalBets,
		WonBets:     s.WonBets,
		LostBets:    s.LostBets,
		VoidBets:    s.VoidBets,
		PendingBets: s.PendingBets,
		Staked:      s.Staked,
		Profit:      s.Profit,
		WinRate:     s.WinRate,
		ROI:         s.ROI,
	}
}

func toLeaderboardRowDTO(r domain.LeaderboardRow) leaderboardRowDTO {
	return leaderboardRowDTO{
		UserID:      r.UserID,
		DisplayName: r.DisplayName,
		AvatarURL:   r.AvatarURL,
		TotalBets:   r.TotalBets,
		WonBets:     r.WonBets,
		Staked:      r.Staked,
		Profit:      r.Profit,
		ROI:         r.ROI,
	}
}
