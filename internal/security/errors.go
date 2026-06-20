package security

import "errors"

// Сентинельные ошибки пакета security. Слой service должен мапить их на
// доменные/HTTP-коды, а не передавать текст bcrypt наружу.

// ErrInvalidPassword — пароль не совпадает с хэшем (или хэш повреждён).
// Намеренно не различаем эти случаи: атакующий не должен понимать причину отказа.
var ErrInvalidPassword = errors.New("invalid password")
