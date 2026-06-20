package handler

import (
	"errors"
	"log"
	"net/http"

	"fanticbet/internal/domain"

	"github.com/gin-gonic/gin"
)

// errorBody — единый формат ответа об ошибке (conventions.md:144-148).
// Отдаётся как {"error": {"code": "...", "message": "..."}}.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// respondError шлёт ответ об ошибке в едином формате.
// code — машинный код (validation_error, unauthorized и т.д. из conventions.md:150-162),
// message — человекочитаемое сообщение (на русском для пользователя).
func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": errorBody{Code: code, Message: message}})
}

// respondJSON шлёт успешный ответ. Обёртка для единообразия с respondError.
func respondJSON(c *gin.Context, status int, data any) {
	c.JSON(status, data)
}

// mapDomainErr переводит доменную ошибку сервиса в HTTP-ответ. Возвращает true,
// если ошибка распознана и ответ уже отправлен; false — если ошибка «не наша»
// и caller должен отдать 500 internal_error.
//
// Соответствие кодов — conventions.md:150-162:
//   - ErrInvalidCredentials / ErrTokenRevoked / ErrTokenExpired → 401 unauthorized
//   - ErrNotFound                              → 404 not_found
//   - ErrConflict                              → 409 conflict
func mapDomainErr(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrTokenRevoked),
		errors.Is(err, domain.ErrTokenExpired):
		// Не уточняем причину — единое сообщение для всех auth-ошибок.
		respondError(c, http.StatusUnauthorized, "unauthorized", "Неверные учётные данные или токен")
		return true
	case errors.Is(err, domain.ErrNotFound):
		respondError(c, http.StatusNotFound, "not_found", "Ресурс не найден")
		return true
	case errors.Is(err, domain.ErrConflict):
		respondError(c, http.StatusConflict, "conflict", "Конфликт: ресурс уже существует")
		return true
	}
	// Нераспознанная ошибка — логируем с контекстом, клиенту — generic 500.
	log.Printf("handler: path=%s method=%s: %v", c.Request.URL.Path, c.Request.Method, err)
	return false
}

// respondInternalError — единый 500-й ответ. Вызывается, когда mapDomainErr
// вернул false (неожиданная ошибка).
func respondInternalError(c *gin.Context) {
	respondError(c, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
}
