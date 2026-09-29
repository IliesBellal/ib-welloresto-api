# Import de carte par photo — Étape 2 : remise à niveau de la couche IA

Deuxième étape du découpage de [cadrage-import-carte-photo-ia.md](cadrage-import-carte-photo-ia.md) (§ 5.3 et § 9). Elle rend `internal/ai` capable de servir la future tâche `menu_ocr` : image en entrée, JSON contraint par schéma en sortie, modèle récent. Elle corrige au passage des défauts qui touchent déjà la traduction et l'upsell.

**Statut :** implémenté et commité le 2026-09-29, non déployé (§ 4, § 5).

---

## 1. Constats de départ (vérifiés dans le code)

| # | Constat | Conséquence |
|---|---|---|
| C1 | `buildAIRegistry` ([routes.go](../cmd/api/routes.go)) construit chaque provider une fois avec un modèle vide, et `CompletionRequest` n'a pas de champ modèle | `AI_TASK_*_MODEL` n'a **aucun effet** : la traduction et l'upsell tournent toujours sur le défaut du provider (`claude-haiku-4-5`) |
| C2 | Le provider Anthropic lit `Content[0].Text` | Sur les modèles actuels (réflexion toujours active), le premier bloc est un bloc de réflexion, donc la réponse serait vide |
| C3 | `temperature` est envoyée dès qu'elle est > 0 | Les modèles actuels la rejettent (400). Sans effet pour `menu_ocr`, qui n'en fixe pas. Mais la traduction la code en dur à 0,3 |
| C4 | Délai HTTP fixe par provider (30 s) | Trop court pour une photo de carte dense ; impossible d'allonger pour une seule tâche |
| C5 | Pas d'image dans `CompletionRequest` | — |
| C6 | JSON obtenu par consigne + retrait des balises de code | Fragile ; l'API propose des sorties structurées par schéma sur tous les modèles actuels |
| C7 | `stop_reason` jamais lu | Réponse tronquée ou refus indiscernables d'une réponse normale |

---

## 2. Décisions

### D1 — Le provider d'une tâche applique ses réglages (C1)
`Registry.GetProviderForTask` renvoie un provider enveloppé (`taskProvider`). Il remplit `Model`, `Effort` et `Timeout` de la requête depuis la `TaskConfig` **quand l'appelant les a laissés vides** ; une valeur explicite dans la requête l'emporte toujours. `Name()` reste celui du provider réel.

*Pourquoi une enveloppe plutôt que modifier chaque appelant :*
- la traduction et l'upsell n'ont pas à changer ;
- toute tâche future bénéficie automatiquement de ses variables `AI_TASK_*` ;
- les providers restent partagés entre tâches.

`MaxTokens` et `Temperature` ne sont **pas** remplis par l'enveloppe. Les appelants les passent déjà eux-mêmes (la traduction calcule `MaxTokens` et fixe 0,3 en dur), et les remplir changerait leur comportement.

### D2 — Défaut de modèle vide pour la traduction et l'upsell
Leurs défauts `AI_TASK_*_MODEL` passent de `"claude-haiku-4-5"` à vide : le provider applique alors son propre défaut, qui est `claude-haiku-4-5` pour Anthropic, donc **même modèle qu'avant**.

*Pourquoi :* sans ça, maintenant que le modèle est réellement transmis, basculer une de ces tâches sur `openai` par la seule variable `*_PROVIDER` enverrait `claude-haiku-4-5` à OpenAI.

**À vérifier avant déploiement (décision Q3 du cadrage) :** si `AI_TASK_MENU_TRANSLATION_MODEL` ou `AI_TASK_UPSELL_MODEL` sont renseignés en production, cette valeur sera **réellement appliquée** pour la première fois.

### D3 — Délai par requête, porté par le contexte (C4)
Les clients HTTP des providers n'ont plus de `Timeout` fixe. Le délai est posé sur le contexte de chaque requête : `CompletionRequest.Timeout` s'il est renseigné, sinon celui du provider (`ANTHROPIC_TIMEOUT_MS` / `OPENAI_TIMEOUT_MS`, 30 s par défaut). Un délai plus court déjà présent sur le contexte l'emporte : l'upsell garde ses 1,5 s.

Le contexte couvre l'envoi et la lecture complète de la réponse, comme l'ancien `http.Client.Timeout`. Le comportement des tâches existantes ne change pas.

### D4 — `effort`, paramètre Anthropic optionnel
- `CompletionRequest.Effort` est envoyé dans `output_config.effort` uniquement s'il est renseigné. Haiku 4.5 refuse ce paramètre, et les tâches existantes ne le fixent pas.
- La valeur est validée au démarrage (`AIConfig.Validate`) : une faute de frappe dans `AI_TASK_*_EFFORT` empêche l'API de démarrer au lieu de produire des 400 en production.
- OpenAI ignore ce champ.

### D5 — Lecture de la réponse : blocs texte, raison d'arrêt, refus (C2, C7)
- **Blocs texte.** Anthropic : la réponse est la concaténation, dans l'ordre, des blocs `type == "text"`. Les blocs de réflexion sont ignorés. Pas de bloc texte du tout : erreur explicite, avec la raison d'arrêt.
- **Raison d'arrêt.** `CompletionResponse.StopReason` est normalisé sur le vocabulaire Anthropic : `end_turn`, ou `max_tokens` (`ai.StopReasonMaxTokens`) quand la sortie est tronquée. Côté OpenAI, `finish_reason` `length` devient `max_tokens`.
- **Refus.** Un refus n'est jamais une réponse : le provider renvoie une erreur qui enveloppe `ai.ErrRefused` (Anthropic `stop_reason: "refusal"`, avec la catégorie et l'explication ; OpenAI `content_filter`). `menu_ocr` s'en servira pour distinguer un refus d'une panne, sans décompter de crédit dans les deux cas.
- **Compatibilité.** Traduction et upsell (Haiku 4.5, un seul bloc texte) obtiennent exactement le même `Content` qu'avant. Une réponse tronquée donne toujours un JSON invalide, déjà rattrapé par leur nouvelle tentative existante.

### D6 — Images et sorties structurées, côté Anthropic (C5, C6)
- **`CompletionRequest.Images`** (`ai.Image{MediaType, Data}`) : envoyées en blocs `image` base64, **avant** le texte, comme le recommande la doc Anthropic.
  - Une requête sans image garde exactement son `content` texte d'avant (chaîne simple), pour ne rien changer aux tâches existantes.
  - Les formats sont ceux de l'API : JPEG, PNG, GIF, WebP. `menu_ocr` n'enverra que du JPEG normalisé.
- **`CompletionRequest.JSONSchema`** (`json.RawMessage`) : envoyé dans `output_config.format` (`type: json_schema`), à côté de l'`effort` éventuel.
  - Le schéma est transmis **octet pour octet**, sans re-sérialisation, parce que l'API met en cache la grammaire compilée d'un schéma pendant 24 h : un schéma stable évite de payer la compilation à chaque appel.
  - Contraintes du schéma (doc Anthropic) : `additionalProperties: false` sur chaque objet, pas de bornes numériques ni de longueur, pas de récursion. L'appelant vérifie les bornes lui-même.
- **OpenAI** refuse les deux options avec une erreur qui enveloppe `ai.ErrUnsupported`, **avant** tout appel réseau, plutôt que de les ignorer en silence. Basculer `menu_ocr` sur OpenAI par la seule variable produira donc une erreur claire, pas une extraction dégradée. C'est conforme au cadrage : OpenAI est hors périmètre pour cette tâche.

### D7 — Tâche `menu_ocr` déclarée, fermée par défaut
Déclarée dans [config/ai.go](../internal/config/ai.go) :

| Variable | Défaut | Note |
|---|---|---|
| `AI_TASK_MENU_OCR_PROVIDER` | `anthropic` | |
| `AI_TASK_MENU_OCR_MODEL` | `claude-opus-5-5` | Décision Q2 |
| `AI_TASK_MENU_OCR_EFFORT` | `medium` | `none` = paramètre non envoyé (nécessaire pour `claude-haiku-4-5`). Une valeur vide marche aussi, mais tous les hébergeurs ne permettent pas d'en définir une |
| `AI_TASK_MENU_OCR_MAX_TOKENS` | 16000 | Par photo |
| `AI_TASK_MENU_OCR_TIMEOUT_MS` | 180000 | |
| `AI_TASK_MENU_OCR_ENABLED` | **`false`** | S'ouvre d'abord sur staging pour les tests (cadrage § 8.6) |

- **Pas de `temperature`** pour cette tâche : Opus 5.5 la rejette.
- **Aucun appelant pour l'instant.** La tâche est déclarée pour que la porte IA (étape 3) la trouve ; fermée, `GetProviderForTask` renvoie `ErrTaskDisabled`.
- **Coûts :** `claude-opus-5-5` et `claude-sonnet-5-5` sont ajoutés à la table de coût estimé des journaux ([metrics.go](../internal/ai/metrics.go)).
  - *Constaté, non corrigé :* l'entrée `claude-haiku-4-5` de cette table (0,00025 / 0,00125 par 1 000 jetons) correspond à un ancien tarif. Haiku 4.5 est à 1 $ / 5 $ par million, soit 0,001 / 0,005. Cela ne fausse que l'estimation affichée dans les journaux.
- **`CLAUDE.md`** mentionne désormais `menu_ocr`, le fait que les réglages par tâche sont réellement appliqués, et le rejet de `temperature` par les modèles récents.

## 3. Implémentation

| Fichier | Changement |
|---|---|
| [ai/provider.go](../internal/ai/provider.go) | `CompletionRequest` : `Model`, `Effort`, `Timeout`, `Images`, `JSONSchema`. `CompletionResponse.StopReason`. `ai.Image`, `ai.ErrRefused`, `ai.ErrUnsupported`, `ai.StopReasonMaxTokens` |
| [ai/config.go](../internal/ai/config.go) | `TaskConfig.Effort`, `TaskConfig.Timeout` ; `Validate` contrôle l'effort |
| [ai/registry.go](../internal/ai/registry.go) | `taskProvider` : enveloppe renvoyée par `GetProviderForTask` (D1) |
| [ai/providers/anthropic.go](../internal/ai/providers/anthropic.go) | Modèle et délai par requête, `output_config` (effort, format), blocs image, lecture des blocs texte, `stop_reason`, refus |
| [ai/providers/openai.go](../internal/ai/providers/openai.go) | Modèle et délai par requête, `finish_reason` normalisé, refus, `ErrUnsupported` pour images et schéma |
| [config/ai.go](../internal/config/ai.go) | Défauts de modèle vides (D2), tâche `menu_ocr` (D7), `parseEffort` |
| [ai/metrics.go](../internal/ai/metrics.go) | Coûts estimés Opus 5.5 et Sonnet 5.5 |
| [CLAUDE.md](../CLAUDE.md) | Couche IA et variables `AI_TASK_MENU_OCR_*` |
| Tests (nouveaux) | [registry_test.go](../internal/ai/registry_test.go), [anthropic_test.go](../internal/ai/providers/anthropic_test.go), [openai_test.go](../internal/ai/providers/openai_test.go), [config/ai_test.go](../internal/config/ai_test.go) : jusqu'ici, ni `internal/ai` ni `internal/config` n'avaient de tests |

**Aucun appelant existant n'a été modifié.** La traduction et l'upsell bénéficient de D1 à D5 sans changement de leur code.

## 4. Vérifications

| Vérification | Résultat |
|---|---|
| `go build ./...`, `go vet` sur `internal/ai/...`, `internal/config`, upsell, traduction | OK |
| Tests unitaires `internal/ai/...` (registre, Anthropic, OpenAI) contre un faux serveur HTTP local | OK. Ils couvrent : modèle par défaut ou imposé ; effort et `temperature` envoyés seulement s'ils sont renseignés ; délai par requête ; `content` texte inchangé sans image ; image avant texte en base64 ; schéma transmis tel quel ; blocs de réflexion ignorés ; `max_tokens` remonté ; refus → `ErrRefused` ; OpenAI refuse images et schéma sans appel réseau |
| Tests `internal/config` : défauts de `menu_ocr`, `EFFORT=none`, défauts de modèle vides | OK |
| Tests de l'upsell (utilisent le registre avec un faux provider) | OK |
| Chaque commit isolément, dans un worktree propre | build + vet + tests IA, config, upsell, menu : OK sur les 4 commits |
| Suite unitaire complète `go test ./internal/... ./cmd/...` | Échecs dans `planning/employees`, `planning/leave`, `planning/swaps` et `ubereats` (`TestUberEatsBYOCStatusUpdate_UsesBrandOrderID`). **Déjà présents sur le commit de base `f3ea367`**, vérifié dans un worktree : sans rapport avec ce chantier |
| **Appel réel à l'API Anthropic** | **Non fait** : pas de `ANTHROPIC_API_KEY` en local. La forme des requêtes (`output_config.format` + `effort`, blocs image) suit la documentation Anthropic, mais n'a pas été confrontée à l'API. Première confrontation réelle : les tests de l'étape 3 sur staging |

**Reste à faire avant le déploiement (cadrage § 8.3) :** vérifier en production `AI_TASK_MENU_TRANSLATION_MODEL` et `AI_TASK_UPSELL_MODEL`. S'ils sont renseignés, ils s'appliqueront pour la première fois (D2).

**Non traité, noté pour plus tard :**
- mise en cache de la consigne système (`cache_control`) : gain estimé de quelques centimes par carte ;
- repli automatique sur un autre modèle en cas de refus (`fallbacks`, bêta Anthropic) : un refus sur une photo de carte est très improbable, et il est déjà traité proprement (D5) ;
- température de la traduction codée en dur (C3) ;
- tarif Haiku obsolète dans `metrics.go` (D7).

## 5. Commits

Commits atomiques sur `staging` :

| Commit | Contenu |
|---|---|
| `a4a1d0b` | Modèle, effort et délai appliqués par tâche (D1–D4) + tests |
| `ed23157` | Lecture des blocs texte, raison d'arrêt, refus (D5) + tests |
| `c7a33d9` | Images et sorties structurées côté Anthropic ; OpenAI refuse explicitement (D6) + tests |
| `5917b60` | Tâche `menu_ocr`, coûts, `CLAUDE.md` (D7) + tests |
| *(ce document)* | Doc de l'étape 2 |

**Statut :** implémenté, vérifié et commité ; non poussé, non déployé.
