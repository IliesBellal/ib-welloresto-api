package menu

import "welloresto-api/internal/models"

// platformProducts aplatit les produits d'une catégorie pour les plateformes
// de livraison (Uber Eats, Deliveroo), qui n'ont pas de notion de groupe
// d'affichage. Même règle que kiosk.flattenKioskProducts et
// scannorder.ComputeGetMenu : un produit groupe n'est jamais envoyé lui-même,
// ses sous-produits le sont comme des articles à part entière.
//
// Écart volontaire avec le kiosk : les sous-produits prennent la place du
// groupe au lieu d'être rejetés en fin de catégorie, car l'ordre envoyé est
// l'ordre affiché au client sur la plateforme.
//
// Les filtres propres à chaque plateforme (drapeau de synchro, prix,
// disponibilité) restent appliqués par le mapper, sous-produit par sous-produit.
func platformProducts(products []models.ProductEntry) []models.ProductEntry {
	out := make([]models.ProductEntry, 0, len(products))
	for _, p := range products {
		isGroup := p.IsProductGroup != nil && *p.IsProductGroup
		if !isGroup {
			out = append(out, p)
		}
		out = append(out, p.SubProducts...)
	}
	return out
}
