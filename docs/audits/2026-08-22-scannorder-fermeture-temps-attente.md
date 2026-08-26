# Fermeture temporaire et temps d'attente supplémentaire — ScanNOrder

Date : 2026-08-22
Origine : demande utilisateur — « la fermeture temporaire et le temps d'attente
supplémentaire temporaire ne semblent pas intégrés à ScanNOrder ».

## Constat initial

L'analyse a montré que le diagnostic de départ était partiellement inexact, et
a révélé un bug plus grave que celui recherché.

| Fonctionnalité | État réel avant cette session |
|---|---|
| Fermeture temporaire ScanNOrder | **Déjà fonctionnelle** de bout en bout (`closed_until`, migration 008), y compris le blocage de commande |
| Invalidation du cache Redis vitrine | **Absente** — fermeture « immédiate » invisible jusqu'à 2 min |
| Temps d'attente supplémentaire ScanNOrder | Inexistant |
| Temps d'attente Uber Eats | `UpdateBusyModeTime` écrit, mais **branché sur aucune route** |
| Temps d'attente Deliveroo | `UpdatePreparationTime` écrit, mais **branché sur aucune route** |
| **Action rapide « Temps d'attente » du POS Flutter** | **Déployée et appelant un endpoint inexistant → 404 en production** |
| Statut `pos_closed` côté web ScanNOrder | Reçu, mais affiché au client en texte technique brut |

Le point le plus important est l'avant-dernier : le POS Flutter embarque déjà
`IntegrationWaitTimeDialog` → `PATCH /integrations/global/wait-time` avec
`{wait_time_minutes, affected_integrations}`
([integration_api.dart](../../../wello_resto_flutter/lib/data/api/integration_api.dart)).
Cette route n'existait pas côté API. L'action rapide était donc en échec
silencieux depuis sa mise en production.

La fermeture temporaire, elle, appelle `PATCH /integrations/global/close-temporary`
qui existe : cette action rapide fonctionnait déjà.

## Analyse des documentations plateformes

Vérification faite après coup, sur demande : « temps d'attente » et « temps
d'attente supplémentaire temporaire » sont **deux notions distinctes**, et les
deux plateformes les exposent séparément.

| | Temps d'attente (permanent) | Supplément temporaire |
|---|---|---|
| **Uber Eats** | `default_prep_time` (secondes) | `delay_config{delay_until, delay_duration}` |
| **Deliveroo** | `PUT .../workload/times` `{busy: 35, quiet: 15}` | `PUT .../workload/mode` `{mode}` |
| **ScanNOrder** | `merchant_parameters.preparation_time` | `extra_prep_minutes` / `extra_prep_until` |

Uber Eats porte les deux notions sur le même endpoint
(`/v1/delivery/store/{id}/update-store-prep-time`) via deux champs mutuellement
exclusifs (pointeurs `omitempty`). Le busy mode est **additif** au temps de base
et **expire seul** grâce à `delay_until`.

Deliveroo sépare en deux endpoints : `workload/times` définit *combien de
minutes vaut chaque mode* (configuration permanente), `workload/mode` définit
*quel mode est actif* (signal opérationnel). Aucun des deux n'a de notion
d'échéance.

### Conclusions

- **Uber Eats : conforme.** `UpdateReadyForPickupTime` (→ `default_prep_time`,
  branché sur `PATCH /integrations/uber-eats`) et `UpdateBusyModeTime`
  (→ `delay_config`, branché sur `/global/wait-time`) sont correctement
  distincts. Rien à corriger.
- **ScanNOrder : conforme.** Permanent et supplément additif temporaire sont
  deux jeux de colonnes distincts.
- **Deliveroo : non conforme** — voir la dette ci-dessous.

### Décision : Deliveroo hors périmètre du supplément temporaire

Vérification faite, **Deliveroo n'a aucun mode occupé temporisé**. `PUT
workload/mode` n'accepte que `{"mode": "BUSY"}` : ni durée, ni `until`, ni
`disableAt`. La documentation marchand est explicite — le mode reste actif
jusqu'à désactivation manuelle, avec un avertissement sur la pénalité de
visibilité si on l'oublie. (Le `disableAt` que l'on croise chez Deliverect est
une planification côté middleware, pas un champ Deliveroo.)

Conséquence : brancher Deliveroo sur `/global/wait-time` aurait produit un
réglage qui ne respecte **ni le supplément demandé** (seul le palier compte, et
c'est Deliveroo qui décide de sa valeur en minutes) **ni sa date de fin** (aucune
expiration). Le restaurateur aurait cru avoir posé un délai borné.

**Décision : la plateforme est exclue de cette action, et le refus est
explicite.** `SetWaitTimeIntegrations` sort avant tout appel et n'ajoute jamais
`deliveroo` à `affected_integrations` — annoncer la plateforme comme traitée
alors que rien n'a été poussé donnerait une fausse assurance en plein service.
Verrouillé par `TestSetWaitTimeIntegrations_DeliverooExcluded`.

Bénéfices de ce choix, au-delà de la justesse :

- Plus aucun mode n'est basculé par l'action temporaire, donc **rien à
  restaurer** : le besoin d'une action « retour à la normale » disparaît, ainsi
  que le risque de laisser un site en BUSY.
- La question de la validité de `MODERATE` cesse d'être bloquante pour cette
  fonctionnalité (elle reste ouverte sur le chemin permanent, inchangé).

Le mode occupé Deliveroo reste accessible au restaurateur depuis l'application
Deliveroo — ce que la modale back-office indique explicitement plutôt que de
laisser la plateforme manquer sans explication.

### Dette restante sur le chemin permanent Deliveroo (non corrigée)

1. **Bug pré-existant.** `DeliverooService.UpdatePreparationTime`, appelé par le
   chemin **permanent** (`PATCH /integrations/deliveroo`), pousse un **mode** au
   lieu des **durées** :

   ```go
   mode := mapPreparationTimeToWorkloadMode(preparationTimeMinutes) // 25 → "MODERATE"
   return s.client.UpdateSiteWorkloadMode(ctx, brandID, siteID, mode)
   ```

   Régler « 25 min » en back-office ne configure donc pas 25 min chez
   Deliveroo : cela bascule le site en mode MODERATE. La valeur stockée dans
   `integration_deliveroo.preparation_time_minutes` ne correspond à rien côté
   Deliveroo. `workload/times` n'est appelé **nulle part** dans le code.

2. **`MODERATE` non confirmé.** La documentation publique n'expose que `busy` et
   `quiet` dans l'exemple `workload/times`. Notre mapping envoie
   `QUIET`/`MODERATE`/`BUSY`, et l'erreur éventuelle est avalée par le log de
   `UpdateDeliverooSettings` : tout réglage entre 16 et 30 min pourrait échouer
   silencieusement. À vérifier sur le portail partenaire (accès authentifié).

**Décision utilisateur : aucune correction du chemin permanent dans ce lot.** Le
correctif (ajout de `UpdateSiteWorkloadTimes` au client, bascule du permanent sur
`workload/times`) modifierait le comportement d'un endpoint en production et fera
l'objet d'un arbitrage séparé. Cette dette est désormais **cantonnée au chemin
permanent** : le supplément temporaire ne touche plus Deliveroo du tout.

## Décisions prises

1. **Nom de route et de champs dictés par le client déjà déployé.** Le plan
   initial prévoyait `/integrations/global/extra-wait-time`. Après découverte du
   code Flutter, l'endpoint a été nommé `/integrations/global/wait-time` avec le
   champ `wait_time_minutes` : le POS existant se met à fonctionner sans
   nouvelle version de l'application. Renommer côté API aurait imposé une
   release mobile pour un gain nul.

2. **Filtrage de l'échéance en SQL, pas en Go.** `extra_prep_minutes` est exposé
   par les requêtes ScanNOrder déjà remis à 0 dès que `extra_prep_until` est
   dépassé (`snoActiveExtraPrepMinutes`), sur le modèle exact de `closed_until`
   dans `GetMerchantStatus`. Comparer en Go aurait obligé à connaître le fuseau
   de rendu du driver ; ici les deux opérandes sont évalués par la base dans la
   même référence. Corollaire : aucune tâche de nettoyage, l'expiration est
   portée par la comparaison elle-même.

3. **Supplément additif, jamais substitutif.** `GetEffectivePrepMinutes` renvoie
   `base + supplément`. Le calcul historique a été isolé tel quel dans
   `computeBasePrepMinutes` : sans supplément actif, la valeur retournée est
   exactement celle d'avant.

4. **Deliveroo : pas d'échéance, et c'est affiché.** Le mode reste actif jusqu'au
   prochain changement ; `applied_until` ne l'engage pas. La limite est affichée
   dans la modale back-office plutôt que masquée. Voir la dette ci-dessus pour
   la confusion permanent/temporaire qui subsiste sur ce canal.

   Le temps de préparation permanent en base n'est **pas** réécrit sur ce
   chemin : ce supplément est opérationnel et éphémère, la configuration du
   marchand doit lui survivre.

5. **Fenêtre par défaut de 60 min.** Le POS n'envoie que le supplément. Sans
   échéance il resterait actif indéfiniment — le contraire d'une action rapide de
   coup de feu. `duration_minutes` reste acceptée en option pour les appelants
   qui veulent piloter la fenêtre.

6. **Invalidation Redis ciblée.** `InvalidateMerchantStatusCache` purge
   `scannorder:merchant:<id>:*` uniquement. Le pattern ne recouvre ni les clés
   menu (`scannorder:merchant:menu:<id>:*`) ni upsell, dont l'invalidation reste
   portée par `InvalidateMerchantMenuCaches`. Appelée seulement quand ScanNOrder
   fait partie des plateformes touchées : Uber Eats et Deliveroo ne lisent pas
   ce cache.

7. **Comportement des plateformes inconnues laissé identique.** Un nom non
   reconnu est logué, ignoré, mais compté dans `affected_integrations` — c'est le
   comportement préexistant de `CloseTemporaryIntegrations`, reproduit à
   l'identique dans le jumeau. Le corriger sur un seul des deux endpoints aurait
   créé une asymétrie ; le corriger sur les deux aurait modifié le comportement
   d'un endpoint en production. Verrouillé par test pour que la symétrie reste
   visible.

8. **Périmètre volontairement exclu** : les commandes `IN` (sur place, QR table)
   ne sont pas soumises au gate `pos_closed` — seules `DELIVERY`/`TAKE_AWAY` le
   sont. Comportement préexistant, non modifié ici, signalé pour arbitrage
   ultérieur.

## Fichiers modifiés

### API (`ib-welloresto-api`)

- `migrations/085_scannorder_extra_prep_time.{up,down}.sql` — colonnes
  `extra_prep_minutes` / `extra_prep_until` sur `scannorder_settings`.
- `internal/models/request_objects.go` — `MerchantRow.ExtraPrepMinutes`.
- `internal/modules/scannorder/repository.go` — `snoActiveExtraPrepMinutes`,
  ajout de la colonne dans `GetMerchantByQR` et les trois branches de
  `GetMerchantsByBrandSlug`.
- `internal/modules/scannorder/models.go` — `BrandMerchantRow.ExtraPrepMinutes`.
- `internal/modules/scannorder/service.go` — `GetEffectivePrepMinutes` scindée,
  propagation dans `GetBrand` et `GetSlots`.
- `internal/infrastructure/redis/client.go` — `InvalidateMerchantStatusCache`.
- `internal/modules/integrations/{models,repository,service,handler}.go` —
  `SetWaitTimeIntegrations`, `SetScanNOrderExtraPrep`, invalidation du cache sur
  les deux endpoints globaux, exposition de `extra_prep_*` dans
  `GET /integrations/scannorder`.
- `cmd/api/routes.go` — route `PATCH /integrations/global/wait-time`, injection
  de `redisClient` dans le service integrations.

### Web ScanNOrder (`wello-resto-scannorder`)

- `src/components/cart/CheckoutFlow.tsx` — cas dédié `pos_closed` (panier
  conservé, message client, pas de redirection Stripe).

### Back-office (`wello-back-office`)

- `src/services/integrationsService.ts` — `setEstablishmentWaitTime` et
  `updatePlatformPreparationTime`.
- `src/components/integrations/EstablishmentOperationsModal.tsx` — **modale
  unique** remplaçant `EstablishmentClosureModal` (supprimée). Parcours en
  quatre temps : choix de l'action → durée → fenêtre d'application (seulement
  quand la notion a un sens) → plateformes.
- `IntegrationsOverview.tsx`, `IntegrationCard.tsx`, `ScanNOrder.tsx` — montage
  de la modale unique aux trois emplacements.

Les plateformes proposées dépendent de l'action, pour ne jamais offrir un
réglage que le canal ne sait pas appliquer :

| Action | Uber Eats | Deliveroo | ScanNOrder | Fenêtre « jusque quand » |
|---|---|---|---|---|
| Définir le temps d'attente | ✅ | ✅ | ❌ (aucun endpoint) | sans objet |
| Temps d'attente supplémentaire | ✅ | ❌ (non supporté) | ✅ | ✅ |
| Fermeture temporaire | ✅ | ✅ | ✅ | la durée *est* l'échéance |

L'action « définir le temps d'attente » n'a pas d'endpoint global : la modale
appelle les `PATCH /integrations/{plateforme}` existants en parallèle.
ScanNOrder en est absent car `merchant_parameters.preparation_time` est
aujourd'hui **en lecture seule** côté API — aucune requête ne l'écrit. Signalé
dans la modale plutôt que masqué.

### POS Flutter (`wello_resto_flutter`)

**Aucune modification.** L'action rapide existait déjà et devient fonctionnelle
par le seul ajout de la route côté API.

## Statut d'exécution

- `go build ./...` : ✅ PASS.
- `go test ./internal/modules/integrations/... ./internal/modules/scannorder/...` :
  ✅ PASS — 4 tests ajoutés (14 sous-cas).
- `npx tsc --noEmit` sur `wello-resto-scannorder` : ✅ PASS.
- `npx tsc --noEmit` sur `wello-back-office` : ✅ PASS.
- `go test ./internal/...` : 2 paquets en échec — `order_life_cycle`
  (build cassé : `customers.NewCustomersService` appelé avec un argument de
  moins) et `planning/employees` (attentes sqlmock désalignées). **Échecs
  pré-existants** : vérifiés identiques sur un worktree détaché à HEAD (6b014e2)
  avant toute modification. Hors périmètre de cette session.

### Tests ajoutés

- `scannorder.TestGetEffectivePrepMinutes_Manual` — cœur de la non-régression :
  temps de base strictement inchangé sans supplément, addition correcte avec,
  supplément expiré (0) et négatif traités comme absents.
- `integrations.TestSetWaitTimeIntegrations_Validation` — refus avant tout appel
  plateforme (supplément ≤ 0, aucune plateforme, fenêtre ≤ 0).
- `integrations.TestSetWaitTimeIntegrations_UnknownPlatform` — plateforme
  inconnue non bloquante, fenêtre par défaut appliquée.
- `integrations.TestNormalizeIntegrationName` — tolérance de nommage partagée
  par les deux endpoints globaux.

Le mode `AUTO` de `GetEffectivePrepMinutes` n'est pas couvert en unitaire : il
délègue à `orderLifeCycleSvc`, non injectable sans base. L'addition du supplément
est commune aux deux modes (appliquée après `computeBasePrepMinutes`), donc
couverte par le chemin `MANUAL`.

## À vérifier en staging avant production

1. **Migration 085** à passer manuellement (pas d'outil de migration dans le
   repo).
2. **Purge Redis ciblée au déploiement** : `scannorder:merchant:*` une fois, pour
   repartir sans entrée périmée écrite par l'ancienne version.
3. **Action rapide POS « Temps d'attente »** : vérifier qu'elle ne renvoie plus
   404 et que le temps annoncé sur la vitrine augmente immédiatement.
4. **Fermeture temporaire** : vérifier que la vitrine bascule sans attendre les
   2 min de TTL, et que le passage de commande affiche le message client et non
   « Erreur API : pos_closed ».
5. **Non-régression POS** : le toggle « Point de vente ouvert/fermé »
   (`merchant_parameters.is_open`) est orthogonal à `closed_until` — déclencher
   une fermeture temporaire ScanNOrder et vérifier que le toggle POS ne bouge pas
   et que le service en salle continue.
6. **Expiration** : après la fenêtre, vérifier que le temps annoncé redevient le
   temps de base sans intervention.

## Suites à arbitrer

1. **Correctif du chemin permanent Deliveroo** (voir dette ci-dessus) : ajout de
   `UpdateSiteWorkloadTimes` (`PUT workload/times`) et bascule du permanent
   dessus. Modifie le comportement de `PATCH /integrations/deliveroo` — à
   arbitrer.
2. **Validité de `MODERATE`** comme valeur de `workload/mode`, à confirmer sur le
   portail partenaire Deliveroo. Impacte le chemin permanent uniquement.
3. **POS Flutter** : `IntegrationWaitTimeDialog` propose encore une puce
   Deliveroo, désormais sans effet (l'API l'ignore et ne la renvoie pas dans
   `affected_integrations`). Aucune régression — le dialogue ne lit pas la
   réponse — mais la puce devrait être retirée à la prochaine release mobile.
3. **Temps de préparation ScanNOrder non pilotable** :
   `merchant_parameters.preparation_time` / `preparation_time_mode` sont lus
   partout, écrits nulle part. Un endpoint manquerait pour compléter l'action
   « définir le temps d'attente » sur ce canal.
4. **Commandes `IN`** non soumises au gate `pos_closed` (comportement
   préexistant).

## Sources

- [Deliveroo — Site API](https://api-docs.deliveroo.com/docs/site) (`workload/mode`, `workload/times`)
- [Uber Eats Marketplace API](https://developer.uber.com/docs/eats/introduction)
- [Uber Eats — Busy Mode (aide marchands)](https://help.uber.com/en/merchants-and-restaurants/article/how-do-i-delay-an-order?nodeId=64ca9a31-7dfd-45c1-91a2-f871a2d0d2b3)
