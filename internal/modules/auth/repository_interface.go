package auth

import "context"

// Repository définit les méthodes d'accès DB utilisées par le service auth.
// Permet de découpler l'implémentation SQL du service.
type Repository interface {
	GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error)
}
