package audit

import (
	"context"
	"encoding/json"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
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
	oldJSON := []byte("null")
	newJSON := []byte("null")
	if oldState != nil {
		if b, err := json.Marshal(oldState); err == nil {
			oldJSON = b
		}
	}
	if newState != nil {
		if b, err := json.Marshal(newState); err == nil {
			newJSON = b
		}
	}

	// Création de l'entrée (sans les hashs, le repo s'en charge)
	logEntry := &models.AuditLog{
		ID:           helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix),
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
