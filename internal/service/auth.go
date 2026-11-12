package service

import (
	"context"
	"strings"
	"time"

	"trade/internal/pkg/idgen"
	"trade/internal/pkg/sign"
	"trade/internal/types"
)

type AuthService struct{ d Deps }

type LoginResult struct {
	Token string     `json:"token"`
	User  types.User `json:"user"`
}

func (s *AuthService) Login(ctx context.Context, username, password string) (LoginResult, error) {
	u, err := s.d.Store.Repos().Users.GetByUsername(ctx, username)
	if err != nil {
		return LoginResult{}, types.ErrUnauthorized
	}
	if u.PasswordHash != sign.HashPassword(password) {
		return LoginResult{}, types.ErrUnauthorized
	}
	token := idgen.Token()
	if err := s.d.Tokens.Set(ctx, token, u.ID+"|"+u.Role, 24*time.Hour); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, User: u}, nil
}

// Verify 校验 token 并返回用户 ID 与角色
func (s *AuthService) Verify(ctx context.Context, token string) (string, string, error) {
	v, err := s.d.Tokens.Get(ctx, token)
	if err != nil {
		return "", "", types.ErrUnauthorized
	}
	parts := strings.SplitN(v, "|", 2)
	if len(parts) != 2 {
		return "", "", types.ErrUnauthorized
	}
	return parts[0], parts[1], nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.d.Tokens.Del(ctx, token)
}
