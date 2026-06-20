package security

import (
	"strings"
	"testing"
)

func TestGenerateRefreshToken(t *testing.T) {
	tok, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken returned error: %v", err)
	}
	// 32 байта → base64url без паддинга = 43 символа.
	if len(tok) != 43 {
		t.Errorf("expected 43 chars (32 bytes base64url), got %d (%q)", len(tok), tok)
	}
}

// TestGenerateRefreshToken_Unique — два токена не совпадают (ГСЧ работает).
func TestGenerateRefreshToken_Unique(t *testing.T) {
	a, _ := GenerateRefreshToken()
	b, _ := GenerateRefreshToken()
	if a == b {
		t.Errorf("two generated tokens must differ, got identical %q", a)
	}
}

// TestHashToken_StableForSameInput — хэш детерминирован (нужно для проверки
// токена при refresh: клиент прислал токен, мы хэшируем и сравниваем с БД).
func TestHashToken_StableForSameInput(t *testing.T) {
	tok := "some-token"
	if HashToken(tok) != HashToken(tok) {
		t.Errorf("HashToken is not deterministic for same input")
	}
}

func TestHashToken_DifferentForDifferentInput(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"разные токены", "token-a", "token-b"},
		{"отличие в один символ", "token", "tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if HashToken(tt.a) == HashToken(tt.b) {
				t.Errorf("HashToken(%q) == HashToken(%q), expected different", tt.a, tt.b)
			}
		})
	}
}

// TestHashToken_LooksLikeBase64URL — не содержит +/= (подсказка, что кодировка
// корректная для URL/cookie).
func TestHashToken_LooksLikeBase64URL(t *testing.T) {
	h := HashToken("test-token")
	if strings.ContainsAny(h, "+/=") {
		t.Errorf("hash %q contains non-URL-safe chars", h)
	}
}
