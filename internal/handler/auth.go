package handler

import (
	"net/http"
	"time"

	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
)

// RefreshCookieName — имя cookie, в которой живёт refresh-токен. httpOnly,
// поэтому JS на фронте его не видит; браузер сам шлёт его на /auth/refresh и
// /auth/logout.
const RefreshCookieName = "refresh_token"

// AuthHandler — HTTP-слой аутентификации по email+паролю. Зависит только от
// сервиса (слои handler→service соблюдены) и конфигурации cookie.
type AuthHandler struct {
	auth         *service.AuthService
	cookieSecure bool          // true в prod (https), false для localhost
	cookieDomain string        // "" — текущий хост
	accessTTL    time.Duration // для поля expires_in в ответе
	refreshTTL   time.Duration // время жизни refresh-cookie
}

// NewAuthHandler собирает хендлер. jwtMgr сюда не нужен: проверкой access-токена
// занимается middleware.AuthRequired, которому менеджер передаётся напрямую.
func NewAuthHandler(auth *service.AuthService, cookieSecure bool, cookieDomain string, accessTTL, refreshTTL time.Duration) *AuthHandler {
	return &AuthHandler{
		auth:         auth,
		cookieSecure: cookieSecure,
		cookieDomain: cookieDomain,
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
	}
}

// --- DTO запросов ---

type registerRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required,min=2"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// refreshRequest — опциональное тело для /auth/refresh. Основной источник —
// httpOnly-cookie, но для удобства Postman/тестов допускаем передачу в теле.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// --- DTO ответа ---

// tokenResponse — тело успешного register/login/refresh. Refresh-токен в теле
// НЕ возвращаем: он только в httpOnly-cookie.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // секунды до истечения access-токена
}

// Register — регистрация по email+паролю. Успех → 200 + access в теле, refresh
// в cookie. Дубликат email → 409 (ErrConflict из сервиса).
//
// @Summary      Регистрация
// @Description  Создаёт пользователя, кошелёк и начисляет signup-бонус. Возвращает access-токен; refresh-токен — в httpOnly-cookie.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      registerRequest  true  "Данные регистрации"
// @Success      200   {object}  tokenResponse
// @Failure      400   {object}  errorResponse
// @Failure      409   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	tokens, err := h.auth.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	h.setRefreshCookie(c, tokens.RefreshToken)
	respondJSON(c, http.StatusOK, h.tokenResponse(tokens.AccessToken))
}

// Login — вход по email+паролю. Неверные данные → 401 (ErrInvalidCredentials).
//
// @Summary      Вход
// @Description  Проверяет email+пароль, возвращает access-токен; refresh-токен — в httpOnly-cookie.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      loginRequest  true  "Учётные данные"
// @Success      200   {object}  tokenResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "validation_error", "Неверные данные запроса")
		return
	}

	tokens, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	h.setRefreshCookie(c, tokens.RefreshToken)
	respondJSON(c, http.StatusOK, h.tokenResponse(tokens.AccessToken))
}

// Refresh — обмен refresh-токена на новый access. Токен читается из cookie
// (или из тела для Postman). Невалидный/отозванный/истёкший → 401.
//
// @Summary      Обновление access-токена
// @Description  Берёт refresh-токен из httpOnly-cookie (или из тела) и выдаёт новый access-токен.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  false  "Refresh-токен (опционально, если нет cookie)"
// @Success      200   {object}  tokenResponse
// @Failure      401   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, ok := h.refreshFromRequest(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Отсутствует refresh-токен")
		return
	}

	tokens, err := h.auth.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	// Refresh не ротируется (тот же токен), но переписываем cookie — обновляем
	// её на случай, если клиент прислал токен в теле, а не в cookie.
	h.setRefreshCookie(c, tokens.RefreshToken)
	respondJSON(c, http.StatusOK, h.tokenResponse(tokens.AccessToken))
}

// Logout — отзыв refresh-токена и очистка cookie. Идемпотентен: отсутствие
// токена не ошибка (сервис трактует как уже-разлогиненного).
//
// @Summary      Выход
// @Description  Отзывает refresh-токен и очищает cookie. Идемпотентно.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  false  "Refresh-токен (опционально, если нет cookie)"
// @Success      200   {object}  map[string]string
// @Failure      500   {object}  errorResponse
// @Router       /auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, ok := h.refreshFromRequest(c)
	if ok {
		if err := h.auth.Logout(c.Request.Context(), refreshToken); err != nil {
			if !mapDomainErr(c, err) {
				respondInternalError(c)
			}
			return
		}
	}

	h.clearRefreshCookie(c)
	respondJSON(c, http.StatusOK, gin.H{"message": "Вы вышли из системы"})
}

// --- Хелперы ---

// tokenResponse собирает тело ответа с access-токеном.
func (h *AuthHandler) tokenResponse(access string) tokenResponse {
	return tokenResponse{
		AccessToken: access,
		TokenType:   "bearer",
		ExpiresIn:   int(h.accessTTL.Seconds()),
	}
}

// setRefreshCookie кладёт refresh-токен в httpOnly-cookie. Secure/Domain — из
// конфига. MaxAge = refreshTTL в секундах. Path="/" — доступна всем эндпоинтам.
func (h *AuthHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		RefreshCookieName,
		token,
		int(h.refreshTTL.Seconds()),
		"/",
		h.cookieDomain,
		h.cookieSecure,
		true, // httpOnly
	)
}

// clearRefreshCookie удаляет cookie (MaxAge<0 — браузер стирает её).
func (h *AuthHandler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(RefreshCookieName, "", -1, "/", h.cookieDomain, h.cookieSecure, true)
}

// refreshFromRequest достаёт refresh-токен: сначала из cookie, затем из тела
// запроса (Postman-удобство). Возвращает false, если токена нет нигде.
func (h *AuthHandler) refreshFromRequest(c *gin.Context) (string, bool) {
	if cookie, err := c.Cookie(RefreshCookieName); err == nil && cookie != "" {
		return cookie, true
	}

	var req refreshRequest
	// Тело опционально: ошибку парсинга игнорируем (нет тела — просто нет токена).
	if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
		return req.RefreshToken, true
	}

	return "", false
}
