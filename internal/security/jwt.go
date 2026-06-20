package security

import (
	"fmt"
	"time"

	"fanticbet/internal/domain"

	"github.com/golang-jwt/jwt/v5"
)

// Claims — полезная нагрузка access-токена. Минимум: идентификатор пользователя
// и роль (для middleware-проверок без обращения к БД на каждый запрос).
// RegisteredClaims добавляет стандартные поля iat/exp/jti и т.п.
type Claims struct {
	UserID int64         `json:"uid"`
	Role   domain.Role   `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager выпускает и парсит access-JWT (HS256). Хранит секрет и TTL;
// не имеет состояния между запросами, поэтому потокобезопасен. Один инстанс
// на процесс — создаётся в main и передаётся в сервисы/мiddleware.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTManager создаёт менеджер. Пустой секрет — ошибка конфигурации, которую
// нельзя пропустить на старте приложения (сравн. config.Load проверяет JWT_SECRET).
func NewJWTManager(secret string, ttl time.Duration) (*JWTManager, error) {
	if secret == "" {
		return nil, fmt.Errorf("JWTManager: empty secret")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("JWTManager: non-positive ttl=%v", ttl)
	}
	return &JWTManager{
		secret: []byte(secret),
		ttl:    ttl,
	}, nil
}

// Issue выпускает access-токен для пользователя. nbf намеренно не ставим:
// он избыточен при наличии iat + короткого TTL, а любой рассинхрон часов
// между серверами ломал бы валидацию свежих токенов. jti не генерируем —
// отзыва access-токенов нет по дизайну (короткий TTL + refresh-ротация;
// явный logout отзывает именно refresh).
func (m *JWTManager) Issue(userID int64, role domain.Role, now time.Time) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			Subject:   fmt.Sprintf("%d", userID),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("JWTManager.Issue user_id=%d: %w", userID, err)
	}
	return signed, nil
}

// Parse проверяет подпись и срок действия токена, возвращает claims.
// При любой проблеме (неверная подпись, истёк, повреждён) — ошибка; caller
// трактуется как неавторизованный. Явная проверка method нужна, чтобы при
// подмене alg (например, на "none") атакующий не прошёл.
func (m *JWTManager) Parse(tokenStr string) (Claims, error) {
	claims := Claims{}

	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		// Допускаем только HS256: любая другая схема — атака/ошибка.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return Claims{}, fmt.Errorf("JWTManager.Parse: %w", err)
	}
	return claims, nil
}
