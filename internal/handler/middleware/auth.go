package middleware

import (
	"net/http"
	"strings"

	"fanticbet/internal/domain"
	"fanticbet/internal/security"

	"github.com/gin-gonic/gin"
)

// Ключи, под которыми middleware кладёт данные пользователя в gin.Context.
// gin.Context.Set требует строковый ключ, поэтому это строковые константы
// (не struct-ключи, как txCtxKey в repository). Централизованы здесь, чтобы
// хендлеры не дублировали «магические строки» — читают через геттеры ниже.
const (
	ctxUserID = "user_id"
	ctxRole   = "role"
)

// AuthRequired проверяет access-JWT из заголовка Authorization: Bearer <token>.
// При успехе кладёт user_id и role в контекст запроса; при любой проблеме
// (нет заголовка, кривой формат, невалидный/просроченный токен) — 401.
func AuthRequired(jwt *security.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			abortUnauthorized(c)
			return
		}

		claims, err := jwt.Parse(token)
		if err != nil {
			abortUnauthorized(c)
			return
		}

		c.Set(ctxUserID, claims.UserID)
		c.Set(ctxRole, claims.Role)
		c.Next()
	}
}

// AdminRequired допускает только администраторов. Ставится ПОСЛЕ AuthRequired
// в цепочке: роль читается из контекста, который проложил AuthRequired.
func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := RoleFromContext(c)
		if !ok {
			// AuthRequired не отработал — значит, мы здесь без аутентификации.
			abortUnauthorized(c)
			return
		}
		if role != domain.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"code": "forbidden", "message": "Недостаточно прав"},
			})
			return
		}
		c.Next()
	}
}

// bearerToken достаёт токен из заголовка Authorization. Возвращает false, если
// заголовка нет или схема не Bearer.
func bearerToken(c *gin.Context) (string, bool) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// abortUnauthorized — единый 401-ответ middleware в формате conventions.md.
func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{"code": "unauthorized", "message": "Требуется авторизация"},
	})
}

// UserIDFromContext возвращает user_id, проложенный AuthRequired. Хендлеры за
// AuthRequired могут полагаться на ok == true.
func UserIDFromContext(c *gin.Context) (int64, bool) {
	v, exists := c.Get(ctxUserID)
	if !exists {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// RoleFromContext возвращает роль, проложенную AuthRequired.
func RoleFromContext(c *gin.Context) (domain.Role, bool) {
	v, exists := c.Get(ctxRole)
	if !exists {
		return "", false
	}
	role, ok := v.(domain.Role)
	return role, ok
}
