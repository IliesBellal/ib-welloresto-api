package config

import (
	"os"
	"testing"
	"time"
)

// unsetForTest retire des variables d'environnement pour la durée du test
// (t.Setenv enregistre la restauration, Unsetenv les rend absentes : getEnv
// distingue une variable absente d'une variable vide).
func unsetForTest(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func TestLoadAIConfig_MenuOCRDefaults(t *testing.T) {
	unsetForTest(t,
		"AI_TASK_MENU_OCR_PROVIDER", "AI_TASK_MENU_OCR_MODEL", "AI_TASK_MENU_OCR_EFFORT",
		"AI_TASK_MENU_OCR_MAX_TOKENS", "AI_TASK_MENU_OCR_TIMEOUT_MS", "AI_TASK_MENU_OCR_ENABLED",
	)

	task := loadAIConfig().Tasks["menu_ocr"]
	if task.Provider != "anthropic" || task.Model != "claude-opus-5-5" || task.Effort != "medium" ||
		task.MaxTokens != 16000 || task.Timeout != 3*time.Minute || task.Enabled {
		t.Errorf("menu_ocr = %+v, want anthropic / claude-opus-5-5 / medium / 16000 / 3m / disabled", task)
	}
	if err := loadAIConfig().Validate(); err != nil {
		t.Errorf("default AI config does not validate: %v", err)
	}
}

func TestLoadAIConfig_MenuOCREffortNone(t *testing.T) {
	t.Setenv("AI_TASK_MENU_OCR_MODEL", "claude-haiku-4-5")
	t.Setenv("AI_TASK_MENU_OCR_EFFORT", "none")

	task := loadAIConfig().Tasks["menu_ocr"]
	if task.Model != "claude-haiku-4-5" || task.Effort != "" {
		t.Errorf("menu_ocr = model %q effort %q, want claude-haiku-4-5 and no effort", task.Model, task.Effort)
	}
}

func TestLoadAIConfig_ExistingTasksKeepProviderDefaultModel(t *testing.T) {
	unsetForTest(t, "AI_TASK_MENU_TRANSLATION_MODEL", "AI_TASK_UPSELL_MODEL")

	tasks := loadAIConfig().Tasks
	if tasks["menu_translation"].Model != "" || tasks["upsell"].Model != "" {
		t.Errorf("models = %q / %q, want empty (provider default)", tasks["menu_translation"].Model, tasks["upsell"].Model)
	}
}
