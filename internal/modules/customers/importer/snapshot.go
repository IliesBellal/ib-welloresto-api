package importer

import (
	"encoding/json"
	"fmt"
	"time"
)

// PreviewSnapshot est ce que la preview dépose sous son token, et ce que le
// commit (phase 4) relira.
//
// Il porte le canonique complet et les statuts/décisions proposées par la
// preview, pas le PreviewResult tel quel envoyé au client : celui-ci est
// recalculable et le back-office l'a déjà reçu en réponse. MerchantID figure
// en clair bien qu'il soit déjà dans la clé Redis : le commit doit pouvoir
// vérifier que le snapshot qu'il relit appartient bien au marchand qui
// l'invoque, sans faire confiance au seul chemin par lequel le token est
// arrivé.
//
// CreatedAt est renseigné par l'appelant (le service), jamais par cette
// fonction : une fonction de sérialisation pure ne doit pas appeler
// time.Now().
type PreviewSnapshot struct {
	MerchantID string    `json:"merchant_id"`
	Provider   string    `json:"provider"`
	CreatedAt  time.Time `json:"created_at"`

	Customers []CanonicalCustomer `json:"customers"`
	Rows      []PreviewRow        `json:"rows"`
}

// Encode sérialise le snapshot pour Redis.
func (s *PreviewSnapshot) Encode() (string, error) {
	payload, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("serialisation du snapshot de preview clients: %w", err)
	}
	return string(payload), nil
}

// DecodePreviewSnapshot relit un snapshot déposé par la preview.
func DecodePreviewSnapshot(payload string) (*PreviewSnapshot, error) {
	var snapshot PreviewSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return nil, fmt.Errorf("lecture du snapshot de preview clients: %w", err)
	}
	return &snapshot, nil
}
