package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"spellfire/server/internal/store"
)

func TestRegisterLoginAuthenticateAndLogout(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	service := New(data, time.Hour)
	ctx := context.Background()
	if _, err := service.Register(ctx, "bad", "short"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("invalid registration error = %v", err)
	}
	token, err := service.Register(ctx, "hero@example.com", "correct horse")
	if err != nil || token == "" {
		t.Fatalf("register token = %q, %v", token, err)
	}
	accountID, err := service.Authenticate(ctx, token)
	if err != nil || accountID == "" {
		t.Fatalf("authenticate = %q, %v", accountID, err)
	}
	principal, err := service.AuthenticatePrincipal(ctx, token)
	if err != nil || principal.AccountID != accountID || principal.Email != "hero@example.com" || principal.Admin {
		t.Fatalf("principal = %#v, %v", principal, err)
	}
	adminService := New(data, time.Hour, " HERO@EXAMPLE.COM ")
	principal, err = adminService.AuthenticatePrincipal(ctx, token)
	if err != nil || !principal.Admin {
		t.Fatalf("configured admin principal = %#v, %v", principal, err)
	}
	if _, err := service.Login(ctx, "hero@example.com", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	second, err := service.Login(ctx, "HERO@example.com", "correct horse")
	if err != nil || second == token {
		t.Fatalf("second token = %q, %v", second, err)
	}
	if err := service.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("logged out authentication error = %v", err)
	}
	if _, err := service.Authenticate(ctx, second); err != nil {
		t.Fatalf("other session invalidated: %v", err)
	}
}
