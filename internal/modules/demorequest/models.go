package demorequest

// CreateDemoRequestRequest est le payload JSON de POST /v1/public/demo-request
// — le formulaire "être rappelé" du site vitrine (DemoForm.astro). Depuis le
// chantier créneaux engageants (2026-09-24), SlotStart est un horaire exact
// choisi parmi ceux retournés par GET /v1/public/demo-request/slots, plus une
// préférence texte libre — voir Service.Create pour la revalidation
// serveur. Website n'est jamais rempli par un humain (masqué en CSS/HTML côté
// site) ; un bot qui remplit tous les champs du DOM s'y fait piéger.
type CreateDemoRequestRequest struct {
	Establishment        string `json:"establishment"`
	EstablishmentAddress string `json:"establishment_address,omitempty"`
	RestaurantType       string `json:"restaurant_type"`
	Phone                string `json:"phone"`
	Email                string `json:"email"`
	Situation            string `json:"situation"`
	SlotStart            string `json:"slot_start"` // RFC3339, ex: "2026-09-29T09:00:00+02:00"
	Website              string `json:"website,omitempty"`
}

// SlotView est une cellule de la grille de créneaux — TOUS les créneaux de
// la fenêtre configurée sont renvoyés (disponibles ou non), pour que le
// tableau du site vitrine affiche les créneaux pris visiblement grisés
// plutôt que silencieusement absents (2026-09-25, demande du fondateur).
type SlotView struct {
	Start     string `json:"start"` // RFC3339
	Available bool   `json:"available"`
}

// AvailableSlotsResponse est la réponse de GET /v1/public/demo-request/slots
// — une liste plate de SlotView ; le regroupement en tableau jours × heures
// est fait côté client (DemoForm.astro), pas ici.
type AvailableSlotsResponse struct {
	Slots []SlotView `json:"slots"`
}
