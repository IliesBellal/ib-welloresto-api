package deliveroo_menu

// MenuWebhookPayload représente le JSON envoyé par Deliveroo lors d'un "menu.upload_result"
type MenuWebhookPayload struct {
	Event    string   `json:"event"`    // Doit être "menu.upload_result"
	MenuID   string   `json:"menu_id"`  // L'ID du menu que tu as envoyé
	SiteIDs  []string `json:"site_ids"` // Les sites affectés
	Status   string   `json:"status"`   // "SUCCESS" ou "FAILURE"
	Errors   []string `json:"errors"`   // Présent si Status == "FAILURE"
	Warnings []string `json:"warnings"` // Optionnel
}
