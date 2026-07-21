package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"spellfire/server/internal/model"
	"spellfire/server/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Service struct {
	store    store.Store
	lifetime time.Duration
}

func New(s store.Store, lifetime time.Duration) *Service {
	return &Service{store: s, lifetime: lifetime}
}

func (s *Service) Register(ctx context.Context, email, password string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") || len(email) > 254 || len(password) < 8 || len(password) > 72 {
		return "", ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err = s.store.CreateAccount(ctx, model.Account{ID: NewID(), Email: email, PasswordHash: hash}); err != nil {
		return "", err
	}
	return s.Login(ctx, email, password)
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	a, err := s.store.AccountByEmail(ctx, strings.TrimSpace(strings.ToLower(email)))
	if err != nil || bcrypt.CompareHashAndPassword(a.PasswordHash, []byte(password)) != nil {
		return "", ErrInvalidCredentials
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err = s.store.CreateSession(ctx, tokenHash(token), a.ID, time.Now().Add(s.lifetime)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrInvalidCredentials
	}
	id, err := s.store.AccountIDBySession(ctx, tokenHash(token), time.Now())
	if err != nil {
		return "", ErrInvalidCredentials
	}
	return id, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, tokenHash(token))
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("system random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
