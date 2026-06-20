package security

import (
	"testing"
	"time"

	"fanticbet/internal/domain"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-do-not-use-in-prod-0123456789"

func TestNewJWTManager_InvalidArgs(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		ttl    time.Duration
	}{
		{"пустой секрет", "", time.Minute},
		{"нулевая ttl", testSecret, 0},
		{"отрицательная ttl", testSecret, -time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewJWTManager(tt.secret, tt.ttl); err == nil {
				t.Errorf("expected error for secret=%q ttl=%v", tt.secret, tt.ttl)
			}
		})
	}
}

func TestJWTManager_IssueAndParse_Roundtrip(t *testing.T) {
	mgr, err := NewJWTManager(testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager error: %v", err)
	}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	tok, err := mgr.Issue(42, domain.RoleAdmin, now)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	claims, err := mgr.Parse(tok)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID: got %d, want 42", claims.UserID)
	}
	if claims.Role != domain.RoleAdmin {
		t.Errorf("Role: got %q, want %q", claims.Role, domain.RoleAdmin)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil")
	}
	if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != 15*time.Minute {
		t.Errorf("TTL: got %v, want 15m", got)
	}
}

// TestJWTManager_Parse_Expired — истёкший токен отклоняется.
func TestJWTManager_Parse_Expired(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, time.Minute)

	past := time.Now().Add(-2 * time.Hour)
	tok, err := mgr.Issue(1, domain.RoleUser, past)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	if _, err := mgr.Parse(tok); err == nil {
		t.Errorf("expected error for expired token, got nil")
	}
}

// TestJWTManager_Parse_WrongSecret — токен, подписанный другим секретом, отклоняется.
func TestJWTManager_Parse_WrongSecret(t *testing.T) {
	mgrA, _ := NewJWTManager("secret-A", time.Minute)
	mgrB, _ := NewJWTManager("secret-B", time.Minute)

	tok, _ := mgrA.Issue(1, domain.RoleUser, time.Now())
	if _, err := mgrB.Parse(tok); err == nil {
		t.Errorf("expected error for token signed with different secret, got nil")
	}
}

// TestJWTManager_Parse_Garbage — мусорная строка отклоняется без паники.
func TestJWTManager_Parse_Garbage(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, time.Minute)

	tests := []string{
		"",
		"not-a-jwt",
		"aaa.bbb.ccc",
	}

	for _, tok := range tests {
		if _, err := mgr.Parse(tok); err == nil {
			t.Errorf("expected error for garbage token %q, got nil", tok)
		}
	}
}

// TestJWTManager_Parse_NoneAlgorithm — атака alg=none должна быть отклонена.
// Подписанный с "none" токен не содержит HMAC-сигнатуры; проверка метода
// в Parse возвращает ошибку, не пуская такую подделку.
func TestJWTManager_Parse_NoneAlgorithm(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, time.Minute)
	now := time.Now()

	claims := Claims{
		UserID: 99,
		Role:   domain.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	noneTok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing none token: %v", err)
	}

	if _, err := mgr.Parse(noneTok); err == nil {
		t.Errorf("alg=none token must be rejected, got nil error")
	}
}
