package auth

import "context"

type Service interface {
	GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error)
}
