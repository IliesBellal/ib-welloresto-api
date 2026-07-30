# Décisions — Majorations planning (nuit / dimanche / jours fériés)

Journal chronologique des décisions prises, sujets abordés et sujets reportés
sur le chantier "brancher `night_shift_multiplier`/`sunday_multiplier` dans le
calcul de masse salariale". Complète `docs/PLANNING_AND_USERS_INTEGRATION_GUIDE.md`
(qui documente le contrat API stabilisé) sans le dupliquer.

---

### Typage `sunday_premium`/`night_premium` — bool sur l'employé, pas un taux (2026-07-30)

- **Déclencheur** : un PATCH `/planning/employees/{id}` envoyait
  `sunday_premium: 1.2` / `night_premium: 1.5` (des coefficients) alors que
  les colonnes sont des `TINYINT(1)` (`employees.sunday_premium`/`night_premium`,
  migration `014_planning_socle.sql`) — échec de désérialisation JSON côté Go
  (`*bool` ne peut pas recevoir un nombre).
- **Constat** : à ce moment-là, aucun calcul ne consommait ces deux champs.
  Le seul calcul de masse salariale existant
  (`internal/modules/planning/performance/service.go`, fonction
  `computeDayMetrics`) ne fait que
  `heures_travaillées × hourly_rate × (1 + employer_charges_pct/100)` — les
  flags premium employé sont de purs champs CRUD, jamais lus par un calcul.
- **Décision : Méthode A retenue** — l'employé porte uniquement
  l'**éligibilité** (bool) ; le **taux** vit au niveau référentiel
  (`planning_settings`/`labor_rules`), pas sur l'employé. Rejeté : stocker
  directement un coefficient par employé (`1` = neutre, `1.2`/`1.5` = majoré).
  Raisons (confirmées par la pratique des grands acteurs RH/planning —
  Skello, Combo, ADP, Workday) :
  - le taux est une règle collective (loi/convention), pas une propriété de
    la personne — un float par employé duplique la même valeur sur N lignes ;
  - historisation : une heure de janvier doit rester payée au taux de
    janvier même si la règle change en juillet ; un float mutable sur
    l'employé réécrit silencieusement l'interprétation du passé ;
  - un override individuel reste possible plus tard via une colonne
    **séparée** et nullable, sans jamais remplacer le bool.
- **Action côté front** : laissée à Ilies (correction du payload pour
  envoyer `true`/`false`). Non traité ici.

### Étape 1 — Référentiel : ajout de `sunday_multiplier` (2026-07-30)

- **Contexte** : asymétrie détectée — `planning_settings`/`labor_rules`
  avaient déjà `night_shift_multiplier` et `holiday_multiplier`, mais aucun
  `sunday_multiplier`.
- **Fait** (par une session Claude Code parallèle, vérifié et validé dans
  celle-ci — build + vet OK) : migration Postgres
  `076_planning_settings_sunday_multiplier` (`numeric(4,2) NOT NULL DEFAULT 1.00`),
  champ `SundayMultiplier` ajouté à `PlanningSettings`/`PlanningSettingsUpdateRequest`
  (`internal/modules/planning/settings/models.go`), branché dans les 3
  requêtes de `repository.go` (insert/select/update).
- **Décision annexe confirmée** : pas de valeur par défaut dans
  `defaultLaborRule("FR")` pour `sunday_multiplier` (contrairement à
  `night_shift_multiplier`/`holiday_multiplier`) — la loi française
  n'impose pas de majoration dominicale standard au niveau national
  (contrairement au repos de nuit). Défaut neutre `1.0`
  (`DefaultSundayMultiplier`), configurable ensuite par établissement.

### Étape 2 — Mode de cumul des majorations (2026-07-30)

- **Sujet** : quand nuit + dimanche (+ éventuellement férié) coïncident sur
  la même heure travaillée, comment cumuler les taux ? Ilies a proposé de
  rendre ça configurable par établissement (à la Skello), plutôt que de
  figer une règle unique dans le code.
- **Recherche marché effectuée** (Skello, Combo, jurisprudence Cass.) :
  trois modes coexistent réellement chez les acteurs du secteur :
  1. **Additif** — les taux s'additionnent (ex. +25 % nuit + 50 % dimanche = +75 %).
  2. **Le plus élevé l'emporte** — un seul taux, le max des deux.
     **C'est le comportement légal par défaut en l'absence de clause
     conventionnelle explicite** (jurisprudence : deux majorations
     distinctes prévues par la convention pour dimanche et jour férié ne se
     cumulent pas sauf disposition contraire).
  3. **Taux combiné fixe** — la convention fixe un taux unique pour la
     combinaison, indépendant de l'addition des deux (ex. "dimanche de
     nuit = +75 %").
- **Décision** : enum `premium_cumulation_mode` sur `planning_settings`
  (`additive` / `highest` / `fixed`), défaut `highest` (= comportement légal
  par défaut). Si `fixed`, un champ additionnel `night_sunday_combined_multiplier`
  porte le taux combiné. **Validé par Ilies.**
- **Fait** : migration Postgres
  `077_planning_settings_premium_cumulation_mode`
  (`premium_cumulation_mode varchar(16) NOT NULL DEFAULT 'highest'`,
  `night_sunday_combined_multiplier numeric(4,2) NULL`) ; champs + constantes +
  `NormalizePremiumCumulationMode`/`IsValidPremiumCumulationMode` ajoutés dans
  `settings/models.go` ; branché dans les 3 requêtes de `settings/repository.go`
  (insert/select/update, nullable scanné via `sql.NullFloat64` comme le reste
  du fichier) ; validation + erreur `ErrPlanningPremiumCumulationModeInvalid`
  (400) ajoutée dans `settings/service.go`/`internal/models/responses_models.go`.
  `go build ./...` et `go vet` passent.
- **Non fait (à noter, pas bloquant)** : `postgres_integration_test.go` de ce
  module n'a pas été étendu avec des assertions sur ces nouveaux champs — ce
  test nécessite une vraie connexion Postgres de dev, non disponible dans
  cette session pour vérifier un ajout avant de l'écrire. Le test existant
  continue de passer (aucune colonne retirée), mais aucune couverture n'existe
  encore spécifiquement pour `sunday_multiplier`/`premium_cumulation_mode`.

### Reporté — Jours fériés HCR : compensation en repos, pas en % (2026-07-30)

- **Constat (recherche marché de l'étape 2)** : la convention collective HCR
  ne prévoit **aucune majoration en pourcentage** pour un jour férié
  travaillé (hors 1ᵉʳ mai) — l'avenant n°2 du 5 février 2007 impose une
  compensation en **nature** (journée de repos compensateur ou indemnisation
  équivalente), pas un multiplicateur du taux horaire.
- **Impact potentiel** : `holiday_multiplier` (déjà présent dans
  `planning_settings`/`labor_rules`) modélise une majoration en %, ce qui ne
  correspond pas au mécanisme HCR pour les jours fériés (à l'inverse du
  1ᵉʳ mai, où une majoration en % s'applique bien).
- **Décision : reporté à long terme.** Hors scope du chantier actuel
  (nuit/dimanche). À retraiter séparément le jour où le calcul de la
  majoration jours fériés est réellement implémenté — probablement avec un
  mode `holiday_compensation_type` (`percentage` / `rest_day` /
  `indemnity`) distinct du multiplicateur pur, plutôt que de forcer
  `holiday_multiplier` à représenter les deux mécanismes.

---

### Back-office — champs éditables sur `/equipe/parametres` (2026-07-30)

- **Contexte** : avant d'attaquer la segmentation temporelle, câblage des
  nouveaux champs référentiel dans le back-office (`wello-back-office`,
  repo séparé), page `EquipeSettings.tsx` (route `/equipe/parametres`),
  carte "Règles de travail". `sunday_multiplier` y était déjà câblé par la
  même session parallèle qui avait fait la migration 076 ; seul
  `premium_cumulation_mode`/`night_sunday_combined_multiplier` manquait.
- **Fait** :
  - `src/types/planning.ts` : type `PremiumCumulationMode`
    (`additive`/`highest`/`fixed`), champs ajoutés à `PlanningSettings` et
    `PlanningSettingsUpdateRequest`.
  - `src/pages/equipe/EquipeSettings.tsx` : `Select` "Cumul nuit / dimanche"
    (3 options avec hint, même pattern que `SwapsCard`/`SWAP_OPTIONS`) +
    champ numérique "Taux combiné nuit + dimanche" affiché **seulement** si
    `premium_cumulation_mode === "fixed"`. Diff/patch générique existant
    (`WorkRulesForm`) réutilisé tel quel, aucune logique de save spécifique
    nécessaire.
  - `npx tsc -p tsconfig.check.json --noEmit` : aucune nouvelle erreur
    introduite (les erreurs présentes dans la sortie sont pré-existantes,
    dans `menuService.ts`/`auth.ts`, sans rapport avec ce changement).
- **Non fait / limite connue** : pas de vérification visuelle en navigateur
  (l'app back-office nécessite une session authentifiée marchand que je
  n'ai pas dans cette session) — seule la vérification par typecheck a été
  faite. À valider visuellement par Ilies avant de considérer l'étape
  définitivement close.
- **Hors scope, non touché** : le repo `wello-back-office` a de nombreux
  autres fichiers modifiés/non commités par ailleurs (`EmployeesModal.tsx`,
  `MemberSheet.tsx`, `ContractTab.tsx`, etc.) — travail en cours d'une autre
  session, non lié à ce chantier, laissé tel quel.

### Segmentation temporelle nuit/dimanche/férié (2026-07-30)

- **Design validé par Ilies avant codage** (y compris le prorata pause) :
  calcul en Go (pas SQL), buckets exclusifs `normal`/`night`/`sunday`/
  `night_sunday` (4, scope volontairement limité à ce que
  `premium_cumulation_mode` gouverne) + `holiday` en compteur marginal
  indépendant (report HCR toujours valable, cf. entrée plus haut).
- **Fait** :
  - `performance/segmentation.go` (nouveau) : `PremiumSegments` (5 compteurs
    en secondes), `segmentInterval` (découpe un intervalle jour par jour,
    classe nuit via `nightWindow`, dimanche via `Weekday()`, férié via une
    map pré-résolue), `applyBreakProration` (répartition proportionnelle du
    `break_minutes` des shifts sur les 4 buckets exclusifs + réduction
    proportionnelle du compteur férié marginal). Fonctions pures, aucune I/O.
  - `performance/segmentation_test.go` (nouveau, 10 tests) : shift plein
    jour, shift à cheval sur minuit entrant un dimanche (vérifie la
    ventilation 2h normal / 2h nuit-samedi / 2h nuit+dimanche), dimanche
    journée sans nuit, férié marginal, fenêtre nuit ne traversant pas minuit
    (cas de config inhabituel), intervalle nul/négatif, prorata pause
    (nominal, no-op à 0, clamp à 0 si pause > brut, réduction du marginal
    férié). Premier test unitaire du module `performance` (jusqu'ici seul
    `postgres_integration_test.go` existait, DB réelle requise).
  - `performance/repository.go` : nouvel interface `SettingsReader`
    (`GetOrCreateSettings` pour la fenêtre nuit, `ListPlanningHolidays` pour
    le calendrier férié — un seul appel par plage plutôt qu'un lookup par
    date) ; `NewRepository` prend désormais ce 3ᵉ paramètre. Deux nouvelles
    méthodes de fetch **brut** (pas d'agrégation SQL) :
    `ListPlannedShiftIntervals` (une ligne par shift, `shift_date`/
    `start_time`/`end_time`/`break_minutes` recomposés en Go via
    `combineDateAndClock` — plus besoin de la branche SQL MySQL/Postgres
    dupliquée pour ce chemin) et `ListWorkedEntryIntervals` (une ligne par
    pointage clôturé, converti dans la vraie timezone marchand via
    `resolveMerchantRangeBounds`, réutilisé tel quel).
  - `performance/models.go` : `PlannedShiftInterval`/`WorkedEntryInterval`
    (lignes brutes) + `RawDayEmployeeMetrics.PlannedPremium`/`WorkedPremium`
    (nouveaux champs additifs, n'affectent aucun calcul existant).
  - `performance/service.go` (`GetRawPerformanceByDay`) : récupère
    settings + fenêtre nuit + calendrier férié + les deux listes
    d'intervalles, segmente chacun, applique le prorata pause côté shifts
    uniquement (les pointages n'ont pas ce problème — une pause y est un
    clock-out/clock-in séparé), agrège dans `PlannedPremium`/`WorkedPremium`.
    Convention d'attribution au jour **conservée à l'identique** de
    l'existant : un shift/pointage à cheval sur minuit reste compté
    entièrement sur son jour de **début** (`shift_date` / jour du
    clock-in), pas splitté entre deux jours dans le rapport — seule la
    classification nuit/dimanche/férié à l'intérieur change, pas le jour
    auquel la ligne est rattachée.
  - `planning_repository.go` : `settingspkg.NewRepository(db)` extrait dans
    une variable partagée, injectée à la fois dans `SettingsRepository` et
    `performancepkg.NewRepository`.
  - `go build ./...`, `go vet ./internal/modules/planning/...` : propres.
    `go test ./internal/modules/planning/performance/...` : tous les
    nouveaux tests passent. `go test ./internal/modules/planning/...` fait
    apparaître 2 paquets en échec (`leave`, `swaps`) — **pré-existants,
    aucun rapport avec ce chantier** (confirmé : ni l'un ni l'autre n'a de
    fichier modifié dans cette session).
- **Non fait (scope volontairement exclu de cette étape, comme annoncé)** :
  `PlannedPremium`/`WorkedPremium` ne sont **pas encore** consommés par
  `computeDayMetrics`/`payrollRaw` — les buckets sont calculés et stockés
  mais n'influencent pas encore `payroll_cost_loaded_cents`. C'est la
  prochaine étape.

### Branchement dans `computeDayMetrics`/`payrollRaw` (2026-07-31)

- **Fait** :
  - `performance/premium_calc.go` (nouveau) : `PremiumConfig` (les 4 réglages
    marchand nécessaires : `NightShiftMultiplier`, `SundayMultiplier`,
    `CumulationMode`, `NightSundayCombinedMultiplier`), `weightedPremiumHours`
    (convertit un `PremiumSegments` en heures pondérées, gated par
    l'éligibilité employé), `combinedMultiplier` (résout le taux du bucket
    `NightSunday` selon `premium_cumulation_mode`, réutilise les constantes
    `settingspkg.PremiumCumulationMode*` plutôt que des strings dupliquées).
    Holiday **volontairement pas appliqué** (pas de flag d'éligibilité
    employé pour ça, mécanisme contesté pour HCR — cf. entrée "Reporté" plus haut).
  - **Décision prise sans validation préalable, à signaler** : si
    `premium_cumulation_mode = "fixed"` mais `night_sunday_combined_multiplier`
    est `NULL` (établissement mal configuré), `combinedMultiplier` retombe
    sur le comportement `"highest"` plutôt que d'utiliser `1.0` silencieux ou
    de faire échouer tout le dashboard performance. À revalider si Ilies
    préfère un autre comportement (erreur explicite ? 1.0 ?).
  - `performance/models.go` : `EmployeeRateRow`/`RawDayEmployeeMetrics`
    enrichis de `SundayPremiumEligible`/`NightPremiumEligible` ;
    `RawPerformanceResponse.Premium PremiumConfig` (nouveau champ).
  - `performance/repository.go` : `ListRatesByEmployee` sélectionne
    désormais aussi `sunday_premium`/`night_premium`.
  - `performance/service.go` : `computeDayMetrics` reçoit `PremiumConfig` en
    paramètre (rempli une fois par `GetRawPerformanceByDay` depuis
    `merchantSettings`, déjà chargé pour la segmentation) ; la ligne
    `payrollRaw += workedDisplayHours * hourlyRate * (1+charges)` devient
    `payrollRaw += weightedPremiumHours(displaySegments, ...) * hourlyRate * (1+charges)`,
    où `displaySegments` suit **exactement** la même bascule
    worked→planned que `workedDisplayHours` (même condition
    `WorkedSeconds <= 0`) — cohérence du fallback préservée.
  - `go build ./...`, `go vet`, tests existants (`segmentation_test.go`) :
    tous verts.
- **Propriété à vérifier en tests (prochaine étape, pas encore fait)** :
  quand aucune majoration ne s'applique (segments 100% `Normal`),
  `weightedPremiumHours` doit redonner exactement `workedDisplayHours` —
  invariant de non-régression important avant de faire confiance au calcul
  pondéré.
- **Non fait, comme convenu** : aucun test de scénario sur
  `computeDayMetrics`/`payrollRaw`/`weightedPremiumHours` pour l'instant —
  Ilies valide avant que je les écrive (branchement d'abord, tests
  ensuite, pour éviter de halluciner des scénarios avant d'avoir confirmé
  le design du calcul lui-même).

### Tests de scénarios sur le calcul pondéré (2026-07-31)

- **Fait** : `premium_calc_test.go` (23 sous-tests : `weightedPremiumHours` ×
  15, `combinedMultiplier` × 8) + `service_premium_test.go` (5 tests
  d'intégration sur `computeDayMetrics`). Détail dans le tableau récapitulatif
  donné à Ilies en conversation. **Tout passe, aucune correction nécessaire.**
- **Ce que ça couvre** : les 3 modes de cumul, les 4 combinaisons
  d'éligibilité sur le bucket qui se chevauche (nuit seule/dimanche seul/
  aucune/les deux), le fallback `fixed` sans taux configuré, le fait que le
  férié reste ignoré du payroll (scope volontaire), la bascule
  worked→planned (même condition que l'ancien code), le gate "pas de taux
  horaire" toujours actif, l'agrégation multi-employés, et la non-fuite du
  poids premium dans `WorkedHours`/`PlannedHours` affichés.
- **Non couvert (limite connue, signalée)** : aucun test d'intégration
  bout-en-bout contre une vraie base ne vérifie que
  `Σ(PlannedPremium buckets)` égale bien `PlannedMinutes` (et pareil côté
  Worked) — les deux sont calculés par des requêtes indépendantes
  (`ListPlannedByDayEmployee` vs `ListPlannedShiftIntervals`). L'équivalence
  a été raisonnée à la main (cf. étape précédente) mais jamais vérifiée
  contre une vraie DB Postgres/MySQL.
- **Effet de bord découvert, hors scope** : `go test ./internal/modules/planning/...`
  fait apparaître 3 paquets en échec — `leave`, `swaps` (déjà notés à
  l'étape précédente) **et `employees`**
  (`TestServiceDeleteEmployeeNullifiesAssignedShifts`,
  `TestServiceDeleteEmployeeReturnsNotFoundWhenEmployeeMissing` — un souci
  d'ordre d'appels sqlmock, "expecting database transaction Begin").
  Confirmé pré-existant : `git status` ne montre aucun fichier modifié dans
  ce paquet à aucun moment de ce chantier. Non traité (hors scope de cette
  session), à signaler à Ilies séparément.

## Prochaines étapes (non décidées / non commencées)

- Tests de scénarios sur `computeDayMetrics`/`payrollRaw`/
  `weightedPremiumHours` (voir entrée juste au-dessus) — Ilies valide avant
  écriture.
- Exposition du détail par tranche dans la réponse `/planning/performance`
  (décision prise : oui, exposer le détail pour affichage front) + mise à
  jour de `docs/backoffice_requirements/PERFORMANCE_API_CONTRACT.md`.
- Tests : `computeDayMetrics`/`payrollRaw` eux-mêmes n'ont toujours aucun
  test unitaire (la segmentation en a désormais, `payrollRaw` pas encore)
  — à créer en même temps que le branchement ci-dessus, avec des cas
  nuit/dimanche/férié/cumul de bout en bout.
