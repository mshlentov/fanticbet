package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS возвращает middleware с white-list разрешённых origin'ов. Отдаём
// Access-Control-Allow-Origin только для известных origin'ов и вместе с
// Allow-Credentials: true — конкретный origin, никогда "*". Связка "*" +
// credentials запрещена спекой и небезопасна.
//
// Запросы без заголовка Origin (curl, Postman, серверные вызовы) — это не CORS:
// просто пропускаем дальше без credential-заголовков.
func CORS(allowed []string) gin.HandlerFunc {
	// Множество для O(1)-проверки. Собираем один раз при инициализации.
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowedSet[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
				// Ответ зависит от Origin — помечаем для корректного кэширования.
				c.Header("Vary", "Origin")
			}
			// Неразрешённый origin: заголовки не ставим, браузер сам заблокирует
			// чтение ответа. Запрос при этом не роняем (preflight ниже обработаем).
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
