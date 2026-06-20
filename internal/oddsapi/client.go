// Package oddsapi — тонкий HTTP-клиент Odds-API.io (https://api.odds-api.io/v3).
//
// Клиент намеренно остаётся тонкой обёрткой над REST: он отвечает за транспорт
// (URL, apiKey, ретраи, лог остатка лимита) и маппинг ответов в доменные
// структуры там, где это однозначно (события). Бизнес-логика (выбор основной
// линии тотала, перевод статусов рынков и т.п.) живёт в воркерах, а не здесь.
//
// Все методы принимают context.Context первым параметром и уважают его отмену
// как при сетевом ожидании, так и в паузах между ретраями.
package oddsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// defaultBaseURL — продакшен-эндпоинт Odds-API. В тестах переопределяется
// через WithBaseURL на адрес httptest-сервера.
const defaultBaseURL = "https://api.odds-api.io/v3"

// Client — HTTP-клиент Odds-API. Потокобезопасен: можно дёргать из нескольких
// воркеров одновременно (http.Client сам по себе потокобезопасен).
type Client struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	maxRetries   int           // число повторов сверх первой попытки
	retryBackoff time.Duration // базовая задержка backoff (растёт экспоненциально)
	logger       *log.Logger
}

// Option — функциональная опция конструктора New.
type Option func(*Client)

// WithBaseURL переопределяет базовый URL (нужно в тестах).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithHTTPClient задаёт собственный http.Client (таймаут, транспорт).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithMaxRetries задаёт число повторов на 5xx/429/сетевых ошибках (по умолчанию 3).
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithRetryBackoff задаёт базовую задержку backoff (по умолчанию 300мс).
func WithRetryBackoff(d time.Duration) Option {
	return func(c *Client) { c.retryBackoff = d }
}

// WithLogger подменяет логгер (по умолчанию log.Default()).
func WithLogger(l *log.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// New создаёт клиент с разумными дефолтами. apiKey обязателен — без него
// все запросы вернут 401 от API.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:      defaultBaseURL,
		apiKey:       apiKey,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		maxRetries:   3,
		retryBackoff: 300 * time.Millisecond,
		logger:       log.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// do выполняет GET-запрос с ретраями и декодирует JSON-ответ в out.
//
// Ретраи: на сетевых ошибках/таймаутах, а также на статусах 429 и 5xx —
// с экспоненциальным backoff. На 4xx (кроме 429) повтор бессмыслен — сразу
// возвращаем *APIError. После каждого ответа логируем x-ratelimit-remaining.
func (c *Client) do(ctx context.Context, path string, query url.Values, out any) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("apiKey", c.apiKey)
	fullURL := c.baseURL + path + "?" + query.Encode()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Перед повтором ждём backoff, не игнорируя отмену контекста.
		if attempt > 0 {
			if err := sleepCtx(ctx, c.backoffDelay(attempt)); err != nil {
				return err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return fmt.Errorf("oddsapi: build request %s: %w", path, err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Контекст отменён/истёк — выходим сразу, ретраить нечего.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("oddsapi: GET %s: %w", path, err)
			c.logger.Printf("oddsapi GET %s: attempt %d failed: %v", path, attempt+1, err)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		c.logRateLimit(path, resp)

		if readErr != nil {
			lastErr = fmt.Errorf("oddsapi: read body %s: %w", path, readErr)
			c.logger.Printf("oddsapi GET %s: attempt %d read error: %v", path, attempt+1, readErr)
			continue
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out != nil {
				if err := json.Unmarshal(body, out); err != nil {
					return fmt.Errorf("oddsapi: decode %s: %w", path, err)
				}
			}
			return nil

		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			// Временная ошибка — ретраим.
			lastErr = &APIError{StatusCode: resp.StatusCode, Body: string(body)}
			c.logger.Printf("oddsapi GET %s: attempt %d got status %d, retrying", path, attempt+1, resp.StatusCode)
			continue

		default:
			// 4xx (кроме 429) — клиентская ошибка, повтор не поможет.
			return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
		}
	}

	return fmt.Errorf("oddsapi: GET %s: exhausted %d retries: %w", path, c.maxRetries, lastErr)
}

// backoffDelay возвращает паузу перед повтором attempt (attempt ≥ 1):
// base, 2·base, 4·base, … — классический экспоненциальный backoff.
func (c *Client) backoffDelay(attempt int) time.Duration {
	return c.retryBackoff * time.Duration(1<<(attempt-1))
}

// logRateLimit пишет остаток лимита из заголовка x-ratelimit-remaining.
// Лимит общий — 5000 req/час, поэтому видеть остаток важно для отладки воркеров.
func (c *Client) logRateLimit(path string, resp *http.Response) {
	remaining := resp.Header.Get("x-ratelimit-remaining")
	if remaining == "" {
		return
	}
	c.logger.Printf("oddsapi GET %s: status=%d ratelimit_remaining=%s", path, resp.StatusCode, remaining)
}

// sleepCtx спит d, но прерывается отменой контекста.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
