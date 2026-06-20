package security

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost — цена хэширования bcrypt. Значение 10 даёт ~50 мс на современном
// железе — достаточно против оффлайн-брутфорса и при этом терпимо для UX логина.
// Меняется только через миграцию (перехэширование при следующем логине).
const bcryptCost = 10

// HashPassword возвращает bcrypt-хэш пароля. Используется при регистрации и
// смене пароля. Возвращает ошибку только при слишком длинном пароле (>72 байт,
// ограничение bcrypt) или сбое самого алгоритма — эти случаи должен обработать
// caller (как правило, 500-я, поскольку вход уже провалидирован).
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("HashPassword: %w", err)
	}
	return string(hash), nil
}

// CheckPassword сравнивает открытый пароль с сохранённым bcrypt-хэшем.
// Возвращает nil при совпадении, ошибку — при несовпадении или повреждённом хэше.
// Сообщения bcrypt намеренно неразличимы, чтобы не подсказывать атакующему
// причину отказа — поэтому оборачиваем в единое ErrInvalidPassword.
func CheckPassword(hash, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
		return ErrInvalidPassword
	}
	return nil
}
