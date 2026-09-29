# Avertissements de démarrage : variables d'environnement manquantes

Ajouté le 2026-09-29 à la demande d'Ilies, pendant le chantier « import de carte par photo ».

## Principe

Deux niveaux au démarrage de l'API :

| Niveau | Où | Comportement |
|---|---|---|
| **Indispensable** | `AppConfig.validate()` ([config.go](../internal/config/config.go)) | `log.Fatal` : l'API ne démarre pas. Inchangé (`POSTGRES_URL`, `GOOGLE_API_KEY`, `R2_PRIVATE_BUCKET`, `PIN_PEPPER`, `FISCAL_SIGNING_KEY`, `SIGNUP_CONTEXT_SIGNING_KEY`, `DEMO_REQUEST_NOTIFICATION_EMAIL`, `GOOGLE_CLIENT_ID`, validité de la config IA) |
| **Optionnel** | `config.LogStartupEnv` ([env_warnings.go](../internal/config/env_warnings.go)), appelé dans [main.go](../cmd/api/main.go) juste après la création du logger | **Avertissement seul** : une ligne `WARN` par variable manquante, avec la fonctionnalité touchée, puis une ligne de synthèse. Jamais fatal |

Même esprit que `database.WarnUnrecordedMigrations`, qui avertit déjà au démarrage sans bloquer.

## Ce qui est signalé

**Variables absentes ou vides** : Redis, clé de chiffrement du PIN des bornes, Brevo, Stripe, Uber Eats, Deliveroo, R2 (hors bucket privé, déjà fatal), URLs publiques (Scan&Order, réinitialisation de mot de passe, planning). La liste complète, avec l'impact de chacune, est dans `optionalEnv`.

**Cas particuliers :**
- **`DB_DIALECT` ≠ `postgres`** : l'API retombe sur MySQL, qui n'est plus en service nulle part.
- **`PUBLIC_RESERVATION_BASE_URL` non définie** : le défaut codé en dur pointe vers **staging** (`https://rsv-staging.onrender.com/`). En production, les liens de réservation partiraient vers la recette.
- **`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`** : signalée seulement si une tâche IA **active** utilise ce fournisseur, avec la liste des tâches touchées. `menu_ocr`, fermée par défaut, ne déclenche donc pas d'avertissement.

**Volontairement non signalées**, parce qu'elles ont un défaut sain : `PORT`, `ENV` (défaut : configuration de production du logger), `LOG_LEVEL`, `LOG_PAYLOAD`, `RBAC_OBSERVE`, `ANALYTICS_DATABASE_URL` (retombe sur `POSTGRES_URL`), `CDS_TOKEN_PEPPER` / `KIOSK_TOKEN_PEPPER` (retombent sur `PIN_PEPPER`), `AI_TASK_*` (défauts documentés).

## Configuration IA effective

Après les avertissements, une ligne `INFO` par tâche IA donne le fournisseur, le **modèle réellement appliqué** (« (défaut du provider) » si la variable est vide), l'effort et l'activation.

C'est ce qui permet de faire, **dans les journaux du premier démarrage**, la vérification demandée par le cadrage (§ 8.3) : depuis l'étape 2, `AI_TASK_MENU_TRANSLATION_MODEL` et `AI_TASK_UPSELL_MODEL` sont réellement appliqués.

## Exemple de sortie

```
WARN  variable d'environnement manquante  {"var": "BREVO_API_KEY", "impact": "aucun e-mail ni SMS envoyé (MFA, réinitialisation de mot de passe, notifications)"}
WARN  variable d'environnement manquante  {"var": "PUBLIC_RESERVATION_BASE_URL", "impact": "non définie : défaut staging (https://rsv-staging.onrender.com/) utilisé pour les liens de réservation"}
WARN  démarrage avec une configuration incomplète  {"variables": 2}
INFO  tâche IA  {"task": "menu_ocr", "provider": "anthropic", "model": "claude-opus-5-5", "effort": "medium", "enabled": false}
INFO  tâche IA  {"task": "menu_translation", "provider": "anthropic", "model": "(défaut du provider)", "effort": "", "enabled": true}
INFO  tâche IA  {"task": "upsell", "provider": "anthropic", "model": "(défaut du provider)", "effort": "", "enabled": true}
```

## Maintenance

Une nouvelle variable optionnelle se déclare dans `optionalEnv`, avec son impact en une phrase. Une variable qui a un défaut sain n'y va pas.

## Vérifications

Tests unitaires [env_warnings_test.go](../internal/config/env_warnings_test.go) :
- environnement complet → aucun avertissement ;
- une variable manquante → un avertissement, avec son impact ;
- `DB_DIALECT` absent ;
- défaut staging de la réservation ;
- clé IA signalée seulement pour les tâches actives.

`go build ./...`, `go vet` et `go test ./internal/config/` : OK. L'API n'a pas été démarrée localement.
