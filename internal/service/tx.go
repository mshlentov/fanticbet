package service

import (
	"context"

	"fanticbet/internal/repository"
)

// TxRunner — минимальная абстракция над транзакциями БД для сервисного слоя.
// Реализация repository.TxManager ему удовлетворяет (см. compile-time маркер ниже).
// Заведён как интерфейс именно здесь, а не в repository, чтобы:
//   - сервисы зависели от интерфейса (а не конкретного типа) — тестируемость
//     через простой мок без поднятия pgxpool;
//   - не тащить зависимость пакета repository в тесты service (mockgen и т.п.).
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// compile-time: *repository.TxManager реализует TxRunner.
var _ TxRunner = (*repository.TxManager)(nil)
