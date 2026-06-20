package service

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// MeResponse — профиль + баланс одним ответом (см. tasks.md:73). Склейка данных
// из двух источников (users + wallets) в один DTO для удобства клиента.
type MeResponse struct {
	User    domain.User
	Balance int64
}

// UserService — операции с собственным профилем пользователя. Не путать с
// AuthService (там — выдача токенов), здесь — чтение/правка профиля и кошелька.
type UserService struct {
	users    repository.UserRepository
	wallets  repository.WalletRepository
	walletTx repository.WalletTransactionRepository
}

func NewUserService(
	users repository.UserRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
) *UserService {
	return &UserService{
		users:    users,
		wallets:  wallets,
		walletTx: walletTx,
	}
}

// GetMe возвращает профиль + текущий баланс. Чтение баланса без блокировки:
// это отображение, а не изменение — FOR UPDATE здесь не нужен.
func (s *UserService) GetMe(ctx context.Context, userID int64) (MeResponse, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return MeResponse{}, fmt.Errorf("UserService.GetMe: %w", err)
	}
	wallet, err := s.wallets.GetByUserID(ctx, userID)
	if err != nil {
		return MeResponse{}, fmt.Errorf("UserService.GetMe balance user_id=%d: %w", userID, err)
	}
	return MeResponse{User: user, Balance: wallet.Balance}, nil
}

// ListTransactions возвращает страницу журнала движений фантиков (новые — первые).
// page начинается с 1; размер страницы задаётся в репозитории (DefaultPageSize).
// Прокси к репозиторию, чтобы хендлер не зависел от repository напрямую (слои).
func (s *UserService) ListTransactions(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error) {
	txs, err := s.walletTx.ListByUser(ctx, userID, page)
	if err != nil {
		return nil, fmt.Errorf("UserService.ListTransactions user_id=%d: %w", userID, err)
	}
	return txs, nil
}

// UpdateProfile меняет отображаемое имя и/или аватар. Поля, которые не нужно
// менять, приходят как nil — тогда сохраняется текущее значение. Пароль и роль
// через этот метод не меняются (для них — отдельные операции).
func (s *UserService) UpdateProfile(ctx context.Context, userID int64, displayName *string, avatarURL *string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("UserService.UpdateProfile: %w", err)
	}

	if displayName != nil {
		user.DisplayName = *displayName
	}
	if avatarURL != nil {
		user.AvatarURL = avatarURL
	}

	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("UserService.UpdateProfile save user_id=%d: %w", userID, err)
	}
	return nil
}
