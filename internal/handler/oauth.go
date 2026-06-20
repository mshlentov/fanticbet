package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// Имя cookie для CSRF-state. TTL = 5 минут: достаточно для завершения OAuth-флоу.
const stateCookieName = "oauth_state"
const stateCookieTTL = 5 * 60

// yandexEndpoint — OAuth2-эндпоинты Яндекс ID. AuthStyleInHeader: client credentials
// передаются как Basic Authorization при обмене кода (требование Яндекса).
var yandexEndpoint = oauth2.Endpoint{
	AuthURL:   "https://oauth.yandex.ru/authorize",
	TokenURL:  "https://oauth.yandex.ru/token",
	AuthStyle: oauth2.AuthStyleInHeader,
}

// vkEndpoint — OAuth2-эндпоинты VK. AuthStyleInParams: client_id/secret передаются
// в теле POST-запроса на обмен кода (стандарт VK).
var vkEndpoint = oauth2.Endpoint{
	AuthURL:   "https://oauth.vk.com/authorize",
	TokenURL:  "https://oauth.vk.com/access_token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// NewOAuthConfigs создаёт готовые oauth2.Config для Яндекс и VK из параметров
// конфига. Вынесено из main, чтобы держать провайдер-специфичные URL рядом
// с кодом их использования.
func NewOAuthConfigs(
	yandexClientID, yandexClientSecret, yandexRedirectURI string,
	vkClientID, vkClientSecret, vkRedirectURI string,
) (yandex *oauth2.Config, vk *oauth2.Config) {
	yandex = &oauth2.Config{
		ClientID:     yandexClientID,
		ClientSecret: yandexClientSecret,
		RedirectURL:  yandexRedirectURI,
		Scopes:       []string{"login:email", "login:info"},
		Endpoint:     yandexEndpoint,
	}
	vk = &oauth2.Config{
		ClientID:     vkClientID,
		ClientSecret: vkClientSecret,
		RedirectURL:  vkRedirectURI,
		Scopes:       []string{"email"},
		Endpoint:     vkEndpoint,
	}
	return
}

// OAuthHandler — HTTP-слой OAuth-авторизации через Яндекс и VK.
// Отвечает за редирект к провайдеру, обработку callback, получение профиля
// и передачу управления OAuthService.
type OAuthHandler struct {
	oauth        *service.OAuthService
	yandex       *oauth2.Config
	vk           *oauth2.Config
	cookieSecure bool
	cookieDomain string
	accessTTL    time.Duration
	refreshTTL   time.Duration
}

func NewOAuthHandler(
	oauth *service.OAuthService,
	yandex *oauth2.Config,
	vk *oauth2.Config,
	cookieSecure bool,
	cookieDomain string,
	accessTTL, refreshTTL time.Duration,
) *OAuthHandler {
	return &OAuthHandler{
		oauth:        oauth,
		yandex:       yandex,
		vk:           vk,
		cookieSecure: cookieSecure,
		cookieDomain: cookieDomain,
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
	}
}

// Login — редирект к OAuth-провайдеру. Генерирует CSRF-state, кладёт его
// в httpOnly-cookie, затем редиректит браузер на страницу авторизации провайдера.
//
// @Summary      OAuth — редирект к провайдеру
// @Description  Начало OAuth-флоу: перенаправляет на страницу авторизации Яндекс или VK. Значения provider: yandex, vk.
// @Tags         auth
// @Param        provider  path  string  true  "Провайдер: yandex или vk"
// @Success      302
// @Failure      400  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /auth/{provider}/login [get]
func (h *OAuthHandler) Login(c *gin.Context) {
	cfg, ok := h.providerConfig(c)
	if !ok {
		return
	}

	state, err := generateState()
	if err != nil {
		respondInternalError(c)
		return
	}

	h.setStateCookie(c, state)
	c.Redirect(http.StatusFound, cfg.AuthCodeURL(state, oauth2.AccessTypeOnline))
}

// Callback — обработка кода от провайдера. Проверяет CSRF-state, обменивает code
// на token провайдера, получает профиль пользователя, вызывает LoginOrRegister.
//
// @Summary      OAuth — callback от провайдера
// @Description  Вызывается провайдером после авторизации. Возвращает access-токен и устанавливает refresh-cookie. Swagger "Try it out" здесь не работает — эндпоинт вызывается браузером, а не напрямую.
// @Tags         auth
// @Param        provider  path   string  true  "Провайдер: yandex или vk"
// @Param        code      query  string  true  "Код авторизации от провайдера"
// @Param        state     query  string  true  "CSRF-state (должен совпасть с cookie)"
// @Success      200  {object}  tokenResponse
// @Failure      400  {object}  errorResponse
// @Failure      401  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /auth/{provider}/callback [get]
func (h *OAuthHandler) Callback(c *gin.Context) {
	cfg, ok := h.providerConfig(c)
	if !ok {
		return
	}

	// Провайдер вернул ошибку (пользователь отказал в доступе и т.п.).
	if errParam := c.Query("error"); errParam != "" {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Авторизация отменена пользователем")
		return
	}

	// Проверка CSRF-state: сравниваем query-параметр с cookie.
	if !h.verifyState(c) {
		respondError(c, http.StatusUnauthorized, "unauthorized", "Неверный state, попробуйте снова")
		return
	}
	h.clearStateCookie(c)

	code := c.Query("code")
	if code == "" {
		respondError(c, http.StatusBadRequest, "validation_error", "Отсутствует код авторизации")
		return
	}

	// Обмен кода на access-токен провайдера.
	token, err := cfg.Exchange(c.Request.Context(), code)
	if err != nil {
		log.Printf("OAuthHandler.Callback: exchange code provider=%s: %v", c.Param("provider"), err)
		respondError(c, http.StatusUnauthorized, "unauthorized", "Не удалось обменять код авторизации")
		return
	}

	// Получение профиля пользователя от провайдера.
	provider := c.Param("provider")
	var info service.OAuthUserInfo
	var domainProvider domain.Provider

	switch provider {
	case "yandex":
		domainProvider = domain.ProviderYandex
		info, err = fetchYandexUser(c.Request.Context(), token.AccessToken)
	case "vk":
		domainProvider = domain.ProviderVK
		info, err = fetchVKUser(c.Request.Context(), token)
	}
	if err != nil {
		log.Printf("OAuthHandler.Callback: fetch user provider=%s: %v", provider, err)
		respondError(c, http.StatusUnauthorized, "unauthorized", "Не удалось получить данные пользователя от провайдера")
		return
	}

	// Три сценария (логин / привязка / регистрация) — в сервисе.
	tokens, err := h.oauth.LoginOrRegister(c.Request.Context(), domainProvider, info)
	if err != nil {
		if !mapDomainErr(c, err) {
			respondInternalError(c)
		}
		return
	}

	h.setRefreshCookie(c, tokens.RefreshToken)
	respondJSON(c, http.StatusOK, tokenResponse{
		AccessToken: tokens.AccessToken,
		TokenType:   "bearer",
		ExpiresIn:   int(h.accessTTL.Seconds()),
	})
}

// --- Провайдер-специфичные функции получения профиля ---

// yandexProfile — ответ Яндекс API GET /info.
type yandexProfile struct {
	ID           string `json:"id"`
	Login        string `json:"login"`
	DisplayName  string `json:"display_name"`
	DefaultEmail string `json:"default_email"`
}

// fetchYandexUser запрашивает профиль пользователя у Яндекс ID API.
// Токен передаётся через заголовок Authorization: OAuth <token>.
func fetchYandexUser(ctx context.Context, accessToken string) (service.OAuthUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://login.yandex.ru/info?format=json", nil)
	if err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchYandexUser new request: %w", err)
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchYandexUser do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchYandexUser: status %d", resp.StatusCode)
	}

	var profile yandexProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchYandexUser decode: %w", err)
	}

	displayName := profile.DisplayName
	if displayName == "" {
		// login почти всегда есть, используем его как запасной вариант.
		displayName = profile.Login
	}
	if displayName == "" {
		displayName = "Пользователь"
	}

	info := service.OAuthUserInfo{
		ProviderUserID: profile.ID,
		DisplayName:    displayName,
	}
	if profile.DefaultEmail != "" {
		email := profile.DefaultEmail
		info.Email = &email
	}

	return info, nil
}

// vkUsersGetResponse — ответ VK API method/users.get.
type vkUsersGetResponse struct {
	Response []struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"response"`
}

// fetchVKUser извлекает профиль пользователя VK.
// user_id и email уже содержатся в ответе на обмен кода (token.Extra);
// имя (first_name, last_name) получаем отдельным запросом к users.get.
func fetchVKUser(ctx context.Context, token *oauth2.Token) (service.OAuthUserInfo, error) {
	// VK возвращает user_id и email прямо в теле ответа на token-exchange.
	// golang.org/x/oauth2 хранит их в token.Extra.
	userIDFloat, ok := token.Extra("user_id").(float64)
	if !ok || userIDFloat == 0 {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchVKUser: missing user_id in token response")
	}
	userIDStr := fmt.Sprintf("%d", int64(userIDFloat))

	// Запрашиваем имя и фамилию через VK API.
	url := fmt.Sprintf(
		"https://api.vk.com/method/users.get?user_ids=%s&fields=first_name,last_name&access_token=%s&v=5.131",
		userIDStr, token.AccessToken,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchVKUser new request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchVKUser do: %w", err)
	}
	defer resp.Body.Close()

	var vkResp vkUsersGetResponse
	if err := json.NewDecoder(resp.Body).Decode(&vkResp); err != nil {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchVKUser decode: %w", err)
	}
	if len(vkResp.Response) == 0 {
		return service.OAuthUserInfo{}, fmt.Errorf("fetchVKUser: empty users.get response for user_id=%s", userIDStr)
	}

	u := vkResp.Response[0]
	displayName := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if displayName == "" {
		displayName = "Пользователь"
	}

	info := service.OAuthUserInfo{
		ProviderUserID: userIDStr,
		DisplayName:    displayName,
	}
	// email может отсутствовать (пользователь не разрешил его передачу).
	if email, ok := token.Extra("email").(string); ok && email != "" {
		info.Email = &email
	}

	return info, nil
}

// --- Вспомогательные методы ---

// providerConfig возвращает oauth2.Config для провайдера из URL-параметра :provider.
// При неизвестном провайдере сам пишет 400 и возвращает false.
func (h *OAuthHandler) providerConfig(c *gin.Context) (*oauth2.Config, bool) {
	switch c.Param("provider") {
	case "yandex":
		return h.yandex, true
	case "vk":
		return h.vk, true
	default:
		respondError(c, http.StatusBadRequest, "validation_error", "Неизвестный провайдер. Допустимые значения: yandex, vk")
		return nil, false
	}
}

// generateState генерирует случайную hex-строку для CSRF-защиты.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateState: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// verifyState сравнивает state из query-параметра callback с state из cookie.
func (h *OAuthHandler) verifyState(c *gin.Context) bool {
	queryState := c.Query("state")
	cookieState, err := c.Cookie(stateCookieName)
	return err == nil && cookieState != "" && queryState == cookieState
}

func (h *OAuthHandler) setStateCookie(c *gin.Context, state string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, state, stateCookieTTL, "/", h.cookieDomain, h.cookieSecure, true)
}

func (h *OAuthHandler) clearStateCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, "", -1, "/", h.cookieDomain, h.cookieSecure, true)
}

func (h *OAuthHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		RefreshCookieName,
		token,
		int(h.refreshTTL.Seconds()),
		"/",
		h.cookieDomain,
		h.cookieSecure,
		true,
	)
}
