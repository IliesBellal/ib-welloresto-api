package order_life_cycle

import (
	"strings"

	"welloresto-api/internal/models"
)

// stockWarningComment place en tête du commentaire d'une commande de plateforme
// la liste de ses produits en rupture, pour que le personnel la voie avant de
// l'accepter (commande ouverte, production). Le texte du client suit.
func stockWarningComment(existing *string, products []models.OrderProductPayload, unavailableIDs []string) *string {
	names := make([]string, 0, len(unavailableIDs))
	seen := map[string]bool{}
	for _, id := range unavailableIDs {
		name := "produit " + id
		for _, p := range products {
			if p.ProductID == id && strings.TrimSpace(p.ProductName) != "" {
				name = strings.TrimSpace(p.ProductName)
				break
			}
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	warning := "Rupture de stock : " + strings.Join(names, ", ") + " (à vérifier avant d'accepter)"
	if existing != nil {
		if rest := strings.TrimSpace(*existing); rest != "" {
			warning += "\n" + rest
		}
	}
	return &warning
}

// customerNoteComment ajoute « Client : <nom> » à la fin du commentaire d'une commande
// de plateforme créée sans client (son enregistrement a échoué) : le personnel reconnaît
// ainsi la commande à son nom.
func customerNoteComment(existing *string, c *models.CustomerRequest) *string {
	name := ""
	if c != nil && c.Name != nil {
		name = strings.TrimSpace(*c.Name)
	}
	if name == "" {
		return existing
	}

	note := "Client : " + name
	if existing != nil {
		if rest := strings.TrimSpace(*existing); rest != "" {
			note = rest + "\n" + note
		}
	}
	return &note
}
