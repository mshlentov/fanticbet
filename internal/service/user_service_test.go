package service

import (
	"context"
	"errors"
	"testing"

	"fanticbet/internal/domain"
)

func TestUserService_GetMe_ReturnsBalance(t *testing.T) {
	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			if id != 12 {
				t.Errorf("queried wrong id: %d", id)
			}
			return domain.User{ID: 12, DisplayName: "Bob", Role: domain.RoleUser}, nil
		},
	}
	wallets := &fakeWalletRepo{
		getByUserFn: func(ctx context.Context, userID int64) (domain.Wallet, error) {
			return domain.Wallet{UserID: userID, Balance: 7500}, nil
		},
	}
	svc := NewUserService(users, wallets, &fakeWalletTxRepo{})

	me, err := svc.GetMe(context.Background(), 12)
	if err != nil {
		t.Fatalf("GetMe error: %v", err)
	}
	if me.User.ID != 12 {
		t.Errorf("user id: got %d, want 12", me.User.ID)
	}
	if me.Balance != 7500 {
		t.Errorf("balance: got %d, want 7500", me.Balance)
	}
}

func TestUserService_GetMe_UserNotFound(t *testing.T) {
	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			return domain.User{}, domain.ErrNotFound
		},
	}
	svc := NewUserService(users, &fakeWalletRepo{}, &fakeWalletTxRepo{})

	_, err := svc.GetMe(context.Background(), 999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserService_UpdateProfile_DisplayName(t *testing.T) {
	currentName := "Old"
	var saved domain.User
	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			return domain.User{ID: 5, DisplayName: currentName, Role: domain.RoleUser}, nil
		},
		updateFn: func(ctx context.Context, u domain.User) error {
			saved = u
			return nil
		},
	}
	svc := NewUserService(users, &fakeWalletRepo{}, &fakeWalletTxRepo{})

	newName := "New"
	if err := svc.UpdateProfile(context.Background(), 5, &newName, nil); err != nil {
		t.Fatalf("UpdateProfile error: %v", err)
	}
	if saved.DisplayName != "New" {
		t.Errorf("saved display_name: got %q, want %q", saved.DisplayName, "New")
	}
}

func TestUserService_UpdateProfile_Avatar(t *testing.T) {
	var saved domain.User
	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			return domain.User{ID: 5, DisplayName: "Name", Role: domain.RoleUser}, nil
		},
		updateFn: func(ctx context.Context, u domain.User) error {
			saved = u
			return nil
		},
	}
	svc := NewUserService(users, &fakeWalletRepo{}, &fakeWalletTxRepo{})

	avatar := "https://cdn.test/avatar.png"
	if err := svc.UpdateProfile(context.Background(), 5, nil, &avatar); err != nil {
		t.Fatalf("UpdateProfile error: %v", err)
	}
	if saved.AvatarURL == nil || *saved.AvatarURL != avatar {
		t.Errorf("saved avatar: got %v, want %q", saved.AvatarURL, avatar)
	}
}

// ListTransactions — прокси к репозиторию: передаёт userID/page и
// возвращает строки как есть.
func TestUserService_ListTransactions_Proxies(t *testing.T) {
	want := []domain.WalletTransaction{
		{ID: 1, UserID: 7, Amount: 10000, Type: domain.TxSignupBonus, BalanceAfter: 10000},
	}
	var gotUserID int64
	var gotPage int
	walletTx := &fakeWalletTxRepo{
		listByUserFn: func(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error) {
			gotUserID = userID
			gotPage = page
			return want, nil
		},
	}
	svc := NewUserService(&fakeUserRepo{}, &fakeWalletRepo{}, walletTx)

	got, err := svc.ListTransactions(context.Background(), 7, 2)
	if err != nil {
		t.Fatalf("ListTransactions error: %v", err)
	}
	if gotUserID != 7 || gotPage != 2 {
		t.Errorf("proxied args: user_id=%d page=%d, want 7/2", gotUserID, gotPage)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("unexpected result: %+v", got)
	}
}

// nil-поля не должны затирать существующие значения.
func TestUserService_UpdateProfile_NilKeepsValues(t *testing.T) {
	var saved domain.User
	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			return domain.User{ID: 5, DisplayName: "KeepMe", Role: domain.RoleUser}, nil
		},
		updateFn: func(ctx context.Context, u domain.User) error {
			saved = u
			return nil
		},
	}
	svc := NewUserService(users, &fakeWalletRepo{}, &fakeWalletTxRepo{})

	// Оба поля nil — сохранение без изменений.
	if err := svc.UpdateProfile(context.Background(), 5, nil, nil); err != nil {
		t.Fatalf("UpdateProfile error: %v", err)
	}
	if saved.DisplayName != "KeepMe" {
		t.Errorf("display_name must be kept, got %q", saved.DisplayName)
	}
}
