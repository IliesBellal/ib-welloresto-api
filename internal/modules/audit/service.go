package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/google/uuid"
)

type AuditService interface {
	// LogChange permet d'enregistrer la modification complète d'un objet (Snapshot)
	LogChange(ctx context.Context, action, resourceType, resourceID string, oldState, newState interface{}) error
}

type auditService struct {
	repo AuditRepository
}

func NewAuditService(repo AuditRepository) AuditService {
	return &auditService{repo: repo}
}

func (s *auditService) LogChange(ctx context.Context, action, resourceType, resourceID string, oldState, newState interface{}) error {
	// 1. Récupération de l'utilisateur depuis le context
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		// En fonction de ta politique stricte, tu peux retourner l'erreur ou
		// loguer avec un UserID "SYSTEM" si c'est un webhook par exemple.
		return fmt.Errorf("audit failed: could not extract user from context: %w", err)
	}

	// 2. Conversion des états en JSON
	var oldJSON, newJSON []byte

	if oldState != nil {
		oldJSON, err = json.Marshal(oldState)
		if err != nil {
			return fmt.Errorf("failed to marshal old state: %w", err)
		}
	}

	if newState != nil {
		newJSON, err = json.Marshal(newState)
		if err != nil {
			return fmt.Errorf("failed to marshal new state: %w", err)
		}
	}

	// 3. Construction de l'objet AuditLog
	logEntry := &models.AuditLog{
		ID:           uuid.New().String(),
		UserID:       user.UserID,
		MerchantID:   user.MerchantID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OldValues:    oldJSON,
		NewValues:    newJSON,
	}

	// 4. Sauvegarde
	return s.repo.InsertLog(ctx, logEntry)
}
