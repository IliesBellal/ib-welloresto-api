package audit

import (
	"context"
	"encoding/json"
	"welloresto-api/internal/models"

	"github.com/google/uuid"
)

type AuditService interface {
	// LogChange permet d'enregistrer la modification complète d'un objet (Snapshot)
	LogChange(ctx context.Context, MerchantID, UserID, action, resourceType, resourceID string, oldState, newState interface{}) error
}

type auditService struct {
	repo AuditRepository
}

func NewAuditService(repo AuditRepository) AuditService {
	return &auditService{repo: repo}
}

func (s *auditService) LogChange(ctx context.Context, MerchantID, UserID, action, resourceType, resourceID string, oldState, newState interface{}) error {
	// Marshalling des données
	var oldJSON, newJSON []byte
	if oldState != nil {
		oldJSON, _ = json.Marshal(oldState)
	}
	if newState != nil {
		newJSON, _ = json.Marshal(newState)
	}

	// Création de l'entrée (sans les hashs, le repo s'en charge)
	logEntry := &models.AuditLog{
		ID:           uuid.New().String(),
		UserID:       UserID,
		MerchantID:   MerchantID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OldValues:    oldJSON,
		NewValues:    newJSON,
	}

	// Appel de la méthode chaînée du repo
	return s.repo.InsertLogWithChain(ctx, logEntry)
}
