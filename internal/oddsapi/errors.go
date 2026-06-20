package oddsapi

import "fmt"

// APIError — ошибка, когда Odds-API вернул не-2xx статус. StatusCode позволяет
// вызывающему отличить 404 (события нет) от 5xx/429 (временный сбой). Body —
// усечённое тело ответа для диагностики.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("oddsapi: unexpected status %d: %s", e.StatusCode, truncate(e.Body, 200))
}

// truncate укорачивает строку до n рун-байт (для лога тела ошибки).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
