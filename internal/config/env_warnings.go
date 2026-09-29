package config

import (
	"os"
	"sort"
	"strings"

	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx"
)

// EnvWarning signale une variable d'environnement absente (ou retombée sur un
// défaut risqué) qui ne bloque pas le démarrage mais désactive ou dégrade une
// fonctionnalité. Les variables indispensables restent fatales dans validate().
type EnvWarning struct {
	Var    string
	Impact string
}

// optionalEnv liste les variables sans lesquelles l'API démarre quand même,
// avec la fonctionnalité qui tombe en panne à l'exécution. Une variable n'y
// figure que si son absence n'est pas déjà rattrapée par un défaut sain
// (PORT, ENV, LOG_LEVEL, ANALYTICS_DATABASE_URL, *_TOKEN_PEPPER…).
var optionalEnv = []EnvWarning{
	{"REDIS_URL", "Redis indisponible : sessions et jetons d'authentification, cache IA, cache du menu"},
	{"KIOSK_PIN_ENCRYPTION_KEY", "chiffrement du code PIN administrateur des bornes impossible"},
	{"BREVO_API_KEY", "aucun e-mail ni SMS envoyé (MFA, réinitialisation de mot de passe, notifications)"},
	{"BREVO_WEBHOOK_TOKEN", "webhooks Brevo non authentifiés"},
	{"STRIPE_API_KEY", "paiements et abonnements Stripe indisponibles"},
	{"STRIPE_ONBOARDING_RETURN_URL", "retour de l'onboarding Stripe Connect cassé"},
	{"STRIPE_ONBOARDING_REFRESH_URL", "relance de l'onboarding Stripe Connect cassée"},
	{"UBER_EATS_BASE_URL", "intégration Uber Eats indisponible"},
	{"UBER_EATS_CLIENT_ID", "intégration Uber Eats indisponible (authentification)"},
	{"UBER_EATS_CLIENT_SECRET", "intégration Uber Eats indisponible (authentification)"},
	{"UBER_EATS_TOKEN_TYPE", "intégration Uber Eats indisponible (jeton)"},
	{"DELIVEROO_BASE_URL", "intégration Deliveroo indisponible"},
	{"DELIVEROO_AUTH_BASE_URL", "intégration Deliveroo indisponible (authentification)"},
	{"DELIVEROO_TOKEN", "intégration Deliveroo indisponible (authentification)"},
	{"DELIVEROO_BASE64_BASIC_AUTH", "appels Deliveroo du module deliveroo non authentifiés"},
	{"R2_ACCESS_KEY_ID", "stockage Cloudflare R2 indisponible (images, fichiers)"},
	{"R2_SECRET_ACCESS_KEY", "stockage Cloudflare R2 indisponible (images, fichiers)"},
	{"R2_ENDPOINT", "stockage Cloudflare R2 indisponible (images, fichiers)"},
	{"R2_BUCKET", "bucket R2 public absent : images produits et médias non enregistrés"},
	{"R2_PUBLIC_BASE_URL", "URLs publiques des images R2 incorrectes"},
	{"SCANNORDER_BASE_URL", "liens et QR codes Scan&Order incorrects"},
	{"PASSWORD_RESET_BASE_URL", "liens de réinitialisation de mot de passe incorrects"},
	{"PUBLIC_PLANNING_BASE_URL", "liens publics du planning incorrects"},
}

// EnvWarnings calcule les avertissements de démarrage. Fonction pure (hors
// lecture de l'environnement) pour être testable.
func EnvWarnings(cfg *AppConfig) []EnvWarning {
	var warnings []EnvWarning
	for _, w := range optionalEnv {
		if os.Getenv(w.Var) == "" {
			warnings = append(warnings, w)
		}
	}

	// MySQL n'est plus en service nulle part (CLAUDE.md) : sans
	// DB_DIALECT=postgres, l'API tenterait une base morte.
	if dbx.ActiveDialect() != dbx.Postgres {
		warnings = append(warnings, EnvWarning{dbx.EnvDialect, "ne vaut pas postgres : dialecte MySQL actif, alors que MySQL n'est plus en service"})
	}

	// Défaut en dur pointant vers staging : en production, les liens de
	// réservation partiraient vers l'environnement de recette.
	if _, ok := os.LookupEnv("PUBLIC_RESERVATION_BASE_URL"); !ok {
		warnings = append(warnings, EnvWarning{"PUBLIC_RESERVATION_BASE_URL", "non définie : défaut staging (" + cfg.Reservation.PublicBaseURL + ") utilisé pour les liens de réservation"})
	}

	// Clé d'un fournisseur IA absente alors qu'une tâche active l'utilise.
	keyVar := map[string]string{"anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY"}
	tasksByProvider := map[string][]string{}
	for name, task := range cfg.AI.Tasks {
		if task.Enabled {
			tasksByProvider[task.Provider] = append(tasksByProvider[task.Provider], name)
		}
	}
	for provider, tasks := range tasksByProvider {
		if cfg.AI.Providers[provider].APIKey != "" {
			continue
		}
		sort.Strings(tasks)
		v := keyVar[provider]
		if v == "" {
			v = provider + " API key"
		}
		warnings = append(warnings, EnvWarning{v, "tâches IA actives en échec : " + strings.Join(tasks, ", ")})
	}

	sort.SliceStable(warnings, func(i, j int) bool { return warnings[i].Var < warnings[j].Var })
	return warnings
}

// LogStartupEnv journalise les avertissements de EnvWarnings, puis la
// configuration effective des tâches IA : modèle réellement appliqué (vide =
// défaut du provider), effort, activation. Jamais fatal.
func LogStartupEnv(log *zap.Logger, cfg *AppConfig) {
	warnings := EnvWarnings(cfg)
	for _, w := range warnings {
		log.Warn("variable d'environnement manquante", zap.String("var", w.Var), zap.String("impact", w.Impact))
	}
	if len(warnings) > 0 {
		log.Warn("démarrage avec une configuration incomplète", zap.Int("variables", len(warnings)))
	}

	names := make([]string, 0, len(cfg.AI.Tasks))
	for name := range cfg.AI.Tasks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		task := cfg.AI.Tasks[name]
		model := task.Model
		if model == "" {
			model = "(défaut du provider)"
		}
		log.Info("tâche IA",
			zap.String("task", name),
			zap.String("provider", task.Provider),
			zap.String("model", model),
			zap.String("effort", task.Effort),
			zap.Bool("enabled", task.Enabled),
		)
	}
}
