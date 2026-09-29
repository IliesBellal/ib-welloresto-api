package config

// ImportAIConfig règle la lecture de carte par photo (porte IA de l'import
// produits). Le modèle, l'effort et l'activation sont ceux de la tâche IA
// menu_ocr (config/ai.go).
type ImportAIConfig struct {
	// DefaultCredits est le nombre d'extractions offertes à un marchand, sans
	// limite journalière (Q9 du cadrage). Le staff Wello peut le changer par
	// marchand (PUT /admin/merchants/{id}/menu-ocr-credits).
	DefaultCredits int
}

func loadImportAIConfig() ImportAIConfig {
	return ImportAIConfig{DefaultCredits: getEnvInt("AI_MENU_OCR_DEFAULT_CREDITS", 10)}
}
