package config

import (
	"strings"
	"testing"

	"welloresto-api/internal/ai"
)

// setAllOptionalEnv renseigne toutes les variables surveillées, pour partir
// d'un environnement « complet » et n'en retirer qu'une par test.
func setAllOptionalEnv(t *testing.T) {
	t.Helper()
	for _, w := range optionalEnv {
		t.Setenv(w.Var, "x")
	}
	t.Setenv("DB_DIALECT", "postgres")
	t.Setenv("PUBLIC_RESERVATION_BASE_URL", "https://rsv.example")
}

func warningVars(ws []EnvWarning) []string {
	var vars []string
	for _, w := range ws {
		vars = append(vars, w.Var)
	}
	return vars
}

func aiConfig(anthropicKey string, tasks map[string]ai.TaskConfig) ai.AIConfig {
	return ai.AIConfig{
		Providers: map[string]ai.ProviderConfig{"anthropic": {APIKey: anthropicKey}, "openai": {}},
		Tasks:     tasks,
	}
}

func TestEnvWarnings_CompleteEnvironmentHasNone(t *testing.T) {
	setAllOptionalEnv(t)
	cfg := &AppConfig{AI: aiConfig("key", map[string]ai.TaskConfig{"upsell": {Provider: "anthropic", Enabled: true}})}

	if ws := EnvWarnings(cfg); len(ws) != 0 {
		t.Errorf("warnings = %v, want none", warningVars(ws))
	}
}

func TestEnvWarnings_MissingOptionalVar(t *testing.T) {
	setAllOptionalEnv(t)
	t.Setenv("BREVO_API_KEY", "")
	cfg := &AppConfig{AI: aiConfig("key", nil)}

	ws := EnvWarnings(cfg)
	if len(ws) != 1 || ws[0].Var != "BREVO_API_KEY" || ws[0].Impact == "" {
		t.Errorf("warnings = %+v, want only BREVO_API_KEY with its impact", ws)
	}
}

func TestEnvWarnings_DialectNotPostgres(t *testing.T) {
	setAllOptionalEnv(t)
	t.Setenv("DB_DIALECT", "")
	cfg := &AppConfig{AI: aiConfig("key", nil)}

	if got := warningVars(EnvWarnings(cfg)); len(got) != 1 || got[0] != "DB_DIALECT" {
		t.Errorf("warnings = %v, want [DB_DIALECT]", got)
	}
}

func TestEnvWarnings_ReservationStagingDefault(t *testing.T) {
	setAllOptionalEnv(t)
	unsetForTest(t, "PUBLIC_RESERVATION_BASE_URL")
	cfg := &AppConfig{AI: aiConfig("key", nil), Reservation: loadReservationConfig()}

	ws := EnvWarnings(cfg)
	if len(ws) != 1 || ws[0].Var != "PUBLIC_RESERVATION_BASE_URL" || !strings.Contains(ws[0].Impact, cfg.Reservation.PublicBaseURL) {
		t.Errorf("warnings = %+v, want PUBLIC_RESERVATION_BASE_URL naming the staging default", ws)
	}
}

func TestEnvWarnings_AIKeyOnlyForEnabledTasks(t *testing.T) {
	setAllOptionalEnv(t)
	cfg := &AppConfig{AI: aiConfig("", map[string]ai.TaskConfig{
		"upsell":           {Provider: "anthropic", Enabled: true},
		"menu_translation": {Provider: "anthropic", Enabled: true},
		"menu_ocr":         {Provider: "anthropic", Enabled: false},
	})}

	ws := EnvWarnings(cfg)
	if len(ws) != 1 || ws[0].Var != "ANTHROPIC_API_KEY" {
		t.Fatalf("warnings = %+v, want only ANTHROPIC_API_KEY", ws)
	}
	if !strings.Contains(ws[0].Impact, "menu_translation, upsell") || strings.Contains(ws[0].Impact, "menu_ocr") {
		t.Errorf("impact = %q, want the enabled tasks only", ws[0].Impact)
	}

	// Aucune tâche active sur OpenAI : sa clé absente n'est pas signalée.
	for _, w := range ws {
		if w.Var == "OPENAI_API_KEY" {
			t.Errorf("OPENAI_API_KEY signalée sans tâche OpenAI active")
		}
	}
}
