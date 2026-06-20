package handler

import (
	"net/http"
	"strconv"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/handler/middleware"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
)

// UserHandler — HTTP-слой работы с собственным профилем (/me). За всеми его
// маршрутами стоит middleware.AuthRequired, поэтому user_id всегда в контексте.
type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// --- DTO ---

// userDTO — представление профиля наружу. PasswordHash намеренно отсутствует:
// domain.User не отдаём напрямую, чтобы хэш не утёк в ответ.
type userDTO struct {
	ID          int64      `json:"id"`
	Email       *string    `json:"email"`
	DisplayName string     `json:"display_name"`
	AvatarURL   *string    `json:"avatar_url"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// meResponse — профиль + баланс одним ответом (tasks.md:73).
type meResponse struct {
	User    userDTO `json:"user"`
	Balance int64   `json:"balance"`
}

type updateProfileRequest struct {
	// Указатели + omitempty: отсутствующее поле = nil = «не менять».
	DisplayName *string `json:"display_name" binding:"omitempty,min=2"`
	AvatarURL   *string `json:"avatar_url"`
}

type transactionResponse struct {
	ID           int64     `json:"id"`
	Amount       int64     `json:"amount"`
	Type         string    `json:"type"`
	BetID        *int64    `json:"bet_id"`
	BalanceAfter int64     `json:"balance_after"`
	CreatedAt    time.Time `json:"created_at"`
}

// GetMe — профиль текущего пользователя + баланс.
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	me, err := h.users.GetMe(c.Request.Context(), userID)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	respondJSON(c, http.StatusOK, meResponse{
		User:    toUserDTO(me.User),
		Balance: me.Balance,
	})
}

// UpdateMe — частичное обновление профиля (display_name, avatar). nil-поля
// сохраняют текущие значения.
func (h *UserHandler) UpdateMe(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	if err := h.users.UpdateProfile(c.Request.Context(), userID, req.DisplayName, req.AvatarURL); err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// После обновления возвращаем актуальный профиль с балансом.
	me, err := h.users.GetMe(c.Request.Context(), userID)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}
	respondJSON(c, http.StatusOK, meResponse{
		User:    toUserDTO(me.User),
		Balance: me.Balance,
	})
}

// Transactions — страница истории движений по кошельку. ?page= (по умолчанию 1).
func (h *UserHandler) Transactions(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}

	page := 1
	if raw := c.Query("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}

	txs, err := h.users.ListTransactions(c.Request.Context(), userID, page)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// Маппинг в DTO. nil-срез отдаём как пустой массив, чтобы клиент всегда
	// получал [], а не null.
	items := make([]transactionResponse, 0, len(txs))
	for _, t := range txs {
		items = append(items, toTransactionDTO(t))
	}
	respondJSON(c, http.StatusOK, gin.H{"page": page, "items": items})
}

// toUserDTO маппит domain.User в DTO без PasswordHash (изоляция секретов).
func toUserDTO(u domain.User) userDTO {
	return userDTO{
		ID:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Role:        string(u.Role),
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

func toTransactionDTO(t domain.WalletTransaction) transactionResponse {
	return transactionResponse{
		ID:           t.ID,
		Amount:       t.Amount,
		Type:         string(t.Type),
		BetID:        t.BetID,
		BalanceAfter: t.BalanceAfter,
		CreatedAt:    t.CreatedAt,
	}
}
