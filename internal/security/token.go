package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// refreshTokenSize — длина случайной энтропии refresh-токена в байтах (256 бит).
// После base64url-кодирования это ~43 символа. 256 бит избыточны против перебора
// и оставляют запас даже при компрометации части токенов.
const refreshTokenSize = 32

// GenerateRefreshToken возвращает новый refresh-токен: 32 случайных байта в
// base64url-кодировке (без паддинга). Токен отдаётся клиенту в httpOnly cookie,
// в БД попадает только его хэш (см. HashToken).
func GenerateRefreshToken() (string, error) {
	b := make([]byte, refreshTokenSize)
	// crypto/rand.Reader — криптостойкий ГСЧ ОС; ошибка крайне маловероятна,
	// но игнорировать её нельзя — иначе токен будет предсказуемым.
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("GenerateRefreshToken: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken возвращает sha256-хэш токена в hex. В БД хранится именно он: при
// компрометации БД (дамп) атакующий не получит сами токены и не сможет
// подделать сессию. sha256 здесь уместен: токен высокоэнтропийный, брутфорс
// хэша невозможен, поэтому медленный KDF (bcrypt/scrypt) не нужен.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
