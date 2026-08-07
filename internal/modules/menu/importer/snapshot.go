package importer

import (
	"encoding/json"
	"fmt"
	"time"
)

// PreviewSnapshot est ce que la preview dépose sous son token, et ce que le
// commit relira.
//
// Il porte le canonique et les décisions proposées, pas le PreviewResult :
// celui-ci est recalculable et le back-office l'a déjà reçu en réponse. Le
// stocker en double ferait grossir la valeur Redis sans rien apporter.
//
// MerchantID y figure en clair bien qu'il soit déjà dans la clé : le commit
// doit pouvoir vérifier que le snapshot qu'il relit appartient bien au marchand
// qui l'invoque, sans faire confiance au chemin par lequel le token est arrivé.
type PreviewSnapshot struct {
	Token      string    `json:"token"`
	MerchantID string    `json:"merchant_id"`
	Provider   string    `json:"provider"`
	CreatedAt  time.Time `json:"created_at"`

	Import    *IntermediateImport `json:"import"`
	Decisions ImportDecisions     `json:"decisions"`
}

// Encode sérialise le snapshot pour Redis.
func (s *PreviewSnapshot) Encode() (string, error) {
	if s == nil || s.Import == nil {
		return "", ErrNilImport
	}

	payload, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("sérialisation du snapshot de preview: %w", err)
	}
	return string(payload), nil
}

// DecodePreviewSnapshot relit un snapshot déposé par la preview.
func DecodePreviewSnapshot(payload string) (*PreviewSnapshot, error) {
	var snapshot PreviewSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return nil, fmt.Errorf("lecture du snapshot de preview: %w", err)
	}
	if snapshot.Import == nil {
		return nil, ErrNilImport
	}
	return &snapshot, nil
}
