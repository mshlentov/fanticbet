package security

import (
	"strings"
	"testing"
)

// TestHashPassword_DeterministicStructure проверяет, что хэш валиден и его
// структура соответствует формату bcrypt ($2a$cost$...).
func TestHashPassword_DeterministicStructure(t *testing.T) {
	hash, err := HashPassword("super-secret-123")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash %q is not a bcrypt hash", hash)
	}
}

// TestHashPassword_DifferentSalt — два вызова дают разные хэши (солится).
func TestHashPassword_DifferentSalt(t *testing.T) {
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Errorf("expected different hashes due to salt, got identical %q", a)
	}
}

func TestCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	tests := []struct {
		name    string
		hash    string
		plain   string
		wantErr bool
	}{
		{
			name:    "верный пароль",
			hash:    hash,
			plain:   "correct-horse-battery",
			wantErr: false,
		},
		{
			name:    "неверный пароль",
			hash:    hash,
			plain:   "wrong-password",
			wantErr: true,
		},
		{
			name:    "пустой пароль против реального хэша",
			hash:    hash,
			plain:   "",
			wantErr: true,
		},
		{
			name:    "повреждённый хэш",
			hash:    "not-a-real-hash",
			plain:   "correct-horse-battery",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPassword(tt.hash, tt.plain)
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
			// При ошибке всегда ErrInvalidPassword — не различаем причины.
			if tt.wantErr && err != ErrInvalidPassword {
				t.Errorf("expected ErrInvalidPassword, got %v", err)
			}
		})
	}
}

// TestCheckPassword_LongPasswordRejected — bcrypt падает на паролях > 72 байт.
// Это ограничение алгоритма; сервис должен отлавливать такие пароли валидацией
// раньше, но сам пакет не должен паниковать.
func TestCheckPassword_TooLongPasswordRejected(t *testing.T) {
	hash, _ := HashPassword("short")

	long := strings.Repeat("a", 100)
	err := CheckPassword(hash, long)
	if err != ErrInvalidPassword {
		t.Errorf("expected ErrInvalidPassword for >72-byte password, got %v", err)
	}
}
