# Vacances actives non répercutées sur scannorder / réservation en ligne

Date : 2026-08-13
Origine : question utilisateur — "scannorder affiche-t-il fermé temporairement
quand des vacances sont actives ?"

## Constat initial

Il existe trois calculs de statut "ouvert/fermé" dans l'API, historiquement
non alignés :

| Consommateur | Fonction | Vérifie `hours_of_operation` | Vérifie `planning_holiday_overrides` (jour férié) | Vérifie `planning_vacation_periods` |
|---|---|---|---|---|
| POS / back-office | `pos.Repository.GetPOSStatus` | ✅ | ✅ | ✅ (déjà en place) |
| scannorder (QR, commande en salle/emporter) | `scannorder.Repository.GetMerchantStatus` | ✅ | ✅ | ❌ (trouvé ici) |
| Réservation en ligne | `reservation.buildComputedAvailability` (→ `GetOperationRanges`) | ✅ | ❌ (pas dans le scope de cette session) | ❌ (trouvé ici) |

Le front scannorder ([openStatus.ts](../../../wello-resto-scannorder/src/lib/utils/openStatus.ts))
affiche déjà correctement "Fermé temporairement" dès que l'API renvoie
`status.is_open = false` pendant la plage horaire théorique — le bug était
entièrement côté API : `is_open` ne devenait jamais `false` à cause d'une
vacance active.

Impact concret avant fix : un restaurateur en vacances était bien affiché
fermé sur son POS, mais pas sur son lien scannorder public — et
`CreateOrder` réutilisant le même `GetMerchantStatus`, une commande pouvait
en théorie être créée pendant les vacances. Idem côté réservation : les
créneaux restaient disponibles et une réservation pouvait être prise pour un
jour de fermeture exceptionnelle.

## Décisions prises

1. **scannorder** : réutiliser telle quelle la méthode existante
   `settingspkg.Repository.ResolveVacationOverlap(ctx, merchantID, instant)`
   (déjà utilisée par `pos.GetPOSStatus`), en l'appelant avec `localNow` juste
   après le calcul de `forcedClosed` (jour férié). Alignement 1:1 avec le
   pattern POS, aucun nouveau concept introduit.

2. **Réservation en ligne** : pas de notion de statut "instantané" ici — la
   disponibilité se calcule par journée demandée (`requestedDate`), qui peut
   être dans le futur. Réutiliser `ResolveVacationOverlap` (basé sur un seul
   instant `now`) aurait été faux : une vacance couvrant demain ne doit pas
   dépendre de l'heure actuelle mais du recouvrement avec la journée
   demandée. Décision : ajouter une nouvelle méthode
   `settingspkg.Repository.ResolveVacationRangeOverlap(ctx, merchantID, rangeStart, rangeEnd)`
   qui teste un chevauchement d'intervalles (`start_at < rangeEnd AND end_at > rangeStart`,
   bornes ouvertes pour ne pas flag le jour qui suit immédiatement la fin
   d'une vacance).
   - Exposée via une nouvelle méthode d'interface
     `ReservationRepository.HasVacationOverlap`, implémentée dans
     `reservationRepository` en déléguant à `settingspkg.NewRepository`
     (même style que `pos`/`scannorder`, qui instancient `settingspkg`
     directement depuis leur propre repository plutôt que de l'injecter).
   - Appelée en tout début de `buildComputedAvailability`, juste après le
     calcul de `dayOfWeek`, avec `[requestedDateTime, requestedDateTime+24h)`
     (minuit local à minuit local, cohérent avec le format naïf
     "heure locale marchand" déjà utilisé par `hours_of_operation` et
     `planning_vacation_periods`). Si chevauchement : retour anticipé
     `[]bookingcore.ComputedSlot{}` — court-circuite avant toute requête sur
     `hours_of_operation`/réservations existantes.
   - Effet en cascade : `GetBookingAvailability` renvoie 0 créneau pour ce
     jour (le front verra "aucune disponibilité", équivalent d'un jour
     fermé) ; `CreateReservation`/`UpdateReservation` (qui appellent la même
     fonction pour valider le créneau demandé) renvoient `slot_unavailable`
     — la réservation est bloquée, pas seulement l'affichage.

3. **Périmètre volontairement exclu** : le rattachement des jours fériés
   (`planning_holiday_overrides` / `ResolvePlanningHoliday`) au module
   réservation n'a pas été traité ici — l'utilisateur a demandé
   spécifiquement les vacances, pas les jours fériés. Signalé pour une
   itération future si besoin.

## Fichiers modifiés

- `internal/modules/scannorder/repository.go` — `GetMerchantStatus` appelle
  désormais `ResolveVacationOverlap`.
- `internal/modules/planning/settings/vacations_repository.go` — ajout de
  `ResolveVacationRangeOverlap`.
- `internal/modules/reservation/repository.go` — ajout de
  `HasVacationOverlap` à l'interface `ReservationRepository` et à son
  implémentation.
- `internal/modules/reservation/service.go` — `buildComputedAvailability`
  court-circuite sur vacance active.

## Statut d'exécution

- `go build ./...` : ✅ PASS (compile sans erreur).
- `go test ./internal/modules/scannorder/... ./internal/modules/reservation/... ./internal/modules/pos/... ./internal/modules/planning/settings/...` :
  ✅ PASS — mais ces trois premiers packages n'ont **aucun test unitaire**
  (seulement des `postgres_integration_test.go` gated derrière un tag de
  build `integration`, non exécutés ici faute de connexion Postgres locale).
  `planning/settings` a des tests unitaires classiques, tous PASS, mais
  aucun ne couvre `ResolveVacationRangeOverlap` (nouveau code non testé
  automatiquement).
- Aucun test manuel effectué contre une base réelle (pas d'environnement
  disponible dans cette session). À vérifier en staging avant mise en
  production : créer une vacance couvrant aujourd'hui, vérifier que
  `get_merchant` (scannorder) et `get_slots` (réservation) reflètent bien la
  fermeture.
