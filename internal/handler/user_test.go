package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"fanticbet/internal/domain"
)

// toUserDTO не должен пропускать PasswordHash в ответ: проверяем и на уровне
// структуры (нет такого поля), и в сериализованном JSON (нет хэша/слова password).
func TestToUserDTO_NoPasswordHash(t *testing.T) {
	email := "user@example.com"
	hash := "$2a$10$secretbcrypthashvalue"
	u := domain.User{
		ID:           7,
		Email:        &email,
		PasswordHash: &hash,
		DisplayName:  "Alice",
		Role:         domain.RoleUser,
	}

	dto := toUserDTO(u)

	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)

	if strings.Contains(out, hash) {
		t.Errorf("password hash leaked into DTO JSON: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "password") {
		t.Errorf("DTO JSON contains 'password': %s", out)
	}
	// Полезные поля на месте.
	if dto.ID != 7 || dto.DisplayName != "Alice" || dto.Email == nil || *dto.Email != email {
		t.Errorf("unexpected DTO: %+v", dto)
	}
}
