# RBAC lot 7 — audit des clients face au modèle de droits

Date : 2026-08-27 · Branche : `staging`

Audit seul, aucune garde modifiée. Ce document croise l'inventaire complet des
appels API des quatre clients (`wello_resto_flutter`, `wello-kiosk`,
`wello-resto-scannorder`, `wello-back-office`) avec `docs/RBAC_ROUTES.md`, en
tenant compte des gardes de groupe (`r.Use(...)`), et classe chaque appel
selon le principe directeur du lot :

> Un droit `*.manage` garde la CONFIGURATION et la CORRECTION, jamais la
> SAISIE COURANTE. Prendre une température est une saisie courante ; modifier
> ou supprimer une mesure enregistrée est une correction. Consulter la carte
> est courant ; modifier un prix est de la configuration.

## Résumé chiffré

- **3 incohérences** trouvées, couvrant **8 routes** au total.
- **0 nouveau droit** proposé — les trois se résolvent en réutilisant des
  droits déjà au catalogue (retrait de garde ou déplacement d'un cran).
- **La plus grave : `POST /haccp/traceability` (et sa lecture) gardées par
  `haccp.manage`.** La traçabilité HACCP (réception de marchandises tracées,
  photo + commentaire) est une obligation légale quotidienne, saisie
  plusieurs fois par jour par n'importe quel employé de cuisine via l'app
  Flutter — exactement l'exemple « relever une température » donné comme
  saisie courante par le principe directeur. Le jour où un rôle « cuisinier »
  est créé sans `haccp.manage`, cet employé perd la capacité d'enregistrer une
  traçabilité obligatoire, alors qu'il garde celle de relever une température
  ou de logger un nettoyage (ces routes-là ne sont pas gardées). C'est
  l'incohérence qui casserait le plus visiblement — et le plus tôt, dès le
  premier jour d'usage réel — le service d'un restaurant.

Les deux autres : `POST /customers/` (création d'un client, gardée
`customers.manage`) et quatre lectures de données de référence sous
`/planning` (gardées par la garde de groupe `staff.schedule.manage`). Détail
en §4.

---

## Méthode

1. Quatre agents de recherche en lecture seule ont parcouru chaque dépôt
   client pour lister tous les appels HTTP vers `ib-welloresto-api`.
2. Chaque route a été recroisée avec `docs/RBAC_ROUTES.md` puis vérifiée dans
   `cmd/api/routes.go` (y compris la lecture directe des blocs `r.Route` et
   des gardes `r.Use`) pour confirmer la garde réellement appliquée — pas
   seulement route par route mais en tenant compte de tout `r.Use(...)` posé
   en tête de groupe.
3. Deux hypothèses initiales ont été vérifiées puis **infirmées** par la
   lecture du code avant d'être écartées de la liste des incohérences (voir
   §3.4 et le détail dans le tableau `/planning`) : mieux vaut une hypothèse
   correctement invalidée par preuve qu'une incohérence non vérifiée.

---

## 1-2-3. Inventaire, gardes et classification par domaine

Convention des tableaux : **Client(s)** = qui appelle réellement la route
aujourd'hui ; **Garde** = ce que `RequirePermission` exige réellement, garde
de groupe comprise ; **Catégorie** = CONSULTATION / SAISIE / CORRECTION /
CONFIGURATION ; **⚠** = incohérence (catégorie CONSULTATION ou SAISIE gardée
par un droit `*.manage`).

### 1.1 `wello-resto-scannorder` — hors périmètre RBAC

Client de commande en ligne, entièrement public. Le groupe `/scannorder`
n'a **aucun** `authMiddleware`, donc aucune `permission.Key` ne peut s'y
appliquer — pas de compte, pas de token, pas de droit à mal placer.
Inventaire complet (12 routes : merchant, brand, menu, produit, upsell,
discounts, loyalty programs, delivery/check, pricing, orders create/get/
cancel) sans aucune classification RBAC pertinente. Une route appelée par le
front (`GET /scannorder/{slug}/customer/{id}`, pré-remplissage du formulaire
de commande) **n'existe pas côté serveur** — l'appel échoue silencieusement
(`.catch(() => {})`) ; à nettoyer côté client, sans lien avec ce lot.

### 1.2 `wello-kiosk` — hors périmètre RBAC

Borne en libre-service. Auth par `KioskAuth` (jeton d'appareil), un
mécanisme entièrement distinct du modèle `permission.Key` scopé utilisateur.
Confirmé dans `docs/RBAC_ROUTES.md` : toutes les routes `/kiosk/*` portent
`aucun` droit RBAC. Inventaire complet (20 endpoints : enrôlement, heartbeat,
PIN admin, menu, commande, paiement Terminal, WebSocket) sans incohérence
possible — il n'y a pas de droit RBAC à ce niveau à mal placer. Note : le
PIN admin de la borne (`POST /kiosk/auth/verify-admin-pin`) est un
mécanisme de step-up propre au device, pas une vérification `permission.Key`
— hors périmètre de ce lot mais à garder en tête si un futur lot RBAC touche
à l'admin de borne.

### 1.3 `/auth`, `/users/profile`, session — flutter + back-office

| Méthode | Route | Client(s) | Objet | Garde | Catégorie |
|---|---|---|---|---|---|
| POST/GET | `/auth/login` | flutter, back-office | Connexion / restauration de session | aucune (public) | n/a |
| POST | `/auth/forgot-password`, `/auth/reset-password` | back-office | Mot de passe oublié | aucune (public) | n/a |
| POST | `/auth/verify`, `/auth/send-verification` ; GET `/auth/mfa/fallback-sms` | back-office | Vérification OTP/MFA | aucune | n/a |
| POST | `/auth/pin`, `/auth/pin/set` | flutter | Connexion par PIN / définir son propre PIN | aucune | SAISIE (soi-même) |
| POST | `/device/token` | flutter | Enregistrer le token push FCM | aucune | SAISIE |
| POST | `/app/version/check` | flutter | Vérifier la version app | aucune (public) | n/a |
| PATCH | `/pos/status` | flutter | Ouvrir/fermer le restaurant aux commandes | **`pos.status.manage`** | CONFIGURATION |
| GET/PATCH/POST | `/users/profile`, `/users/profile/avatar` | back-office | Consulter/éditer son propre profil | aucune | CONSULTATION/SAISIE (soi-même) |
| PATCH | `/users/reset-password` | back-office | Changer son propre mot de passe | aucune | SAISIE (soi-même) |
| GET | `/users/notifications` | back-office | Notifications in-app | aucune | CONSULTATION (soi-même) |

`PATCH /pos/status` est un bon exemple de garde déjà bien posée : un droit
étroit et dédié (`pos.status.manage`), pas noyé dans un `*.manage` généraliste
— cohérent avec le principe (fermer tout l'établissement aux commandes en
ligne est une décision qu'un restaurateur voudrait réellement pouvoir
restreindre). **Pas une incohérence.**

### 1.4 `/users` (hors profil) et `/roles` — back-office uniquement, CONFIGURATION

| Méthode | Route | Objet | Garde | Catégorie |
|---|---|---|---|---|
| GET, POST | `/users/`, POST `/users/create` | Lister / créer des comptes employés | `staff.manage` | CONFIGURATION |
| GET | `/users/linkable-search`, `/users/{id}` | Rechercher/consulter un compte employé | `staff.manage` | CONFIGURATION* |
| GET, PUT | `/users/{id}/rights` | Consulter/remplacer les droits d'un employé | `staff.manage` | CONFIGURATION |
| GET, PATCH | `/users/{id}/member` | Consulter/éditer le volet RH d'un employé | `staff.manage` | CONFIGURATION |
| POST, DELETE | `/users/{id}/merchant-link` | Lier/délier un compte à l'établissement | `staff.manage` / `RequireAdmin` (DELETE) | CONFIGURATION/CORRECTION |
| POST | `/users/{id}/force-reset-password` | Forcer la réinitialisation du mot de passe d'un employé | `RequireAdmin` | CORRECTION |

`*` Consulter la fiche d'un collègue expose des données de compte (droits,
email, statut) plus sensibles qu'un simple annuaire — gardé à dessein, pas
une incohérence même si la route est un GET.

Le nouvel API `/roles`, `/permissions`, `/me/permissions` (lot 6,
`docs/RBAC_ROLES_API.md`) n'est appelé par **aucun** client aujourd'hui — pas
encore consommé côté front. Rien à classer.

### 1.5 `/planning/me` — flutter uniquement, libre-service ✅

Toutes les routes suivantes sont `authMiddleware` seul, **sans** garde de
groupe (confirmé dans `routes.go:949-963` — le bloc `/planning/me` n'a pas de
`r.Use(RequirePermission(...))`, contrairement au bloc `/planning` juste en
dessous) :

| Méthode | Route | Objet | Catégorie |
|---|---|---|---|
| GET | `/planning/me/team-week` | Consulter le planning publié de son équipe | CONSULTATION |
| GET, POST | `/planning/me/leave-requests` | Consulter / poser une demande de congé | CONSULTATION / SAISIE |
| GET, POST | `/planning/me/shift-swap-requests` | Consulter / proposer un échange de shift | CONSULTATION / SAISIE |
| POST | `/planning/me/shift-swap-requests/{id}/accept`, `/reject` | Accepter/refuser un échange qui vous cible | SAISIE |
| GET, POST | `/planning/me/time-entries*` | Consulter / pointer (entrée-sortie) | CONSULTATION / SAISIE |

**Pas d'incohérence.** C'est exactement la frontière attendue par le
principe directeur, et elle est déjà correctement posée dans le routeur —
tout ce que l'énoncé du lot cite comme « opérations d'employé » (consulter
son planning, poser un congé, proposer un échange, pointer) vit ici, hors de
la garde de groupe. Point positif à noter explicitement puisque c'est
justement le risque que la consigne demandait de vérifier.

### 1.6 `/planning` (hors `/me`) — back-office uniquement, garde de groupe `staff.schedule.manage` ⚠ partiel

Confirmé dans `routes.go:965-1048` : `r.Use(middleware.RequirePermission(permission.StaffScheduleManage))`
posé une seule fois en tête de groupe, s'applique à tout ce qui suit sans
distinction. Back-office est le seul client à appeler ce groupe — c'est un
outil manager, jamais un employé en libre-service (ce volet-là est couvert
par `/planning/me`, §1.5).

| Méthode | Route(s) | Objet | Catégorie | ⚠ |
|---|---|---|---|---|
| GET, PUT | `/settings` | Paramétrer le module planning | CONFIGURATION | |
| **GET** | **`/contract-types`** | **Lister les types de contrat (référentiel)** | **CONSULTATION** | **⚠** |
| **GET** | **`/attendance-sources`** | **Lister les sources de pointage (référentiel)** | **CONSULTATION** | **⚠** |
| **GET** | **`/event-types`** | **Lister les types d'événement planning (référentiel)** | **CONSULTATION** | **⚠** |
| **GET** | **`/positions`** | **Lister les postes/fonctions (référentiel)** | **CONSULTATION** | **⚠** |
| POST, PATCH, DELETE | `/positions/{id}` | Créer/éditer/supprimer un poste | CONFIGURATION | |
| GET, POST, PATCH, DELETE | `/shift-templates*` | Gérer les modèles de shift | CONFIGURATION | |
| GET, POST, PATCH, DELETE, `/from-week`, `/preview`, `/instantiate` | `/week-templates*` | Gérer les modèles de semaine | CONFIGURATION | |
| GET, POST, PATCH, DELETE | `/employees*` (hors documents/time-entries) | Gérer les fiches employés (création, édition, lien compte) | CONFIGURATION | |
| GET, POST, DELETE | `/employees/{id}/documents*` | Gérer les documents RH d'un employé | CONFIGURATION/CORRECTION | |
| GET | `/employees/{id}/time-entries`, `/time-entries/current` | Consulter les pointages d'un employé donné | CONSULTATION (RH sensible) | |
| POST | `/employees/{id}/time-entries/start`, `/stop` | **Manager définit manuellement une heure d'entrée/sortie pour un employé** (payload `clock_in_at`/`clock_out_at` explicites, UI « TimeEntryDetailSheet ») | CORRECTION | |
| POST, PATCH, DELETE | `/employees/{id}/time-entries` (création manuelle), `/time-entries/{entry_id}` | Corriger un pointage (`modification_reason` obligatoire — audit légal FR, cf. commentaire code) | CORRECTION | |
| GET, POST, PATCH, DELETE | `/weeks*`, `/weeks/{id}/shifts`, `/shifts*`, publish/unpublish | Construire et publier le planning | CONFIGURATION | |
| GET, PUT, DELETE | `/day-comments*` | Annoter une journée du planning | CONFIGURATION | |
| GET, POST, PATCH, DELETE | `/leave-requests*` | Consulter/instruire les demandes de congé de l'équipe | CONSULTATION sensible + CORRECTION (décision d'approbation) | |
| GET, POST, PATCH, DELETE | `/shift-swap-requests*` | Consulter/instruire les échanges de shift de l'équipe | CONSULTATION sensible + CORRECTION | |
| PUT | `/revenue-forecast` | Définir la prévision de CA | CONFIGURATION | |
| GET | `/performance` | Consulter le coût de la main d'œuvre vs CA | CONSULTATION financière sensible | |

**Sur les 4 lignes marquées ⚠** (`contract-types`, `attendance-sources`,
`event-types`, `positions` en lecture) : vérifié dans le code Go
(`internal/modules/planning/refs/models.go`, `internal/modules/planning/employees/models.go`)
que ces quatre listes ne renvoient que des libellés et métadonnées
d'affichage (`code`/`label`/`sort_order`/`active`, ou `label`/`color` pour
les postes) — aucun champ salaire, taux horaire ou donnée personnelle. Ce
sont des données de référence non sensibles, au même titre que la carte :
consulter la liste des types de contrat ou des postes existants n'a rien
d'une configuration, et rien ne justifie qu'un rôle restreint futur en soit
privé. Elles se trouvent gardées uniquement parce qu'elles vivent dans le
même groupe chi que les 60 autres routes réellement CONFIGURATION —
exactement le mécanisme d'erreur que la consigne du lot pointait du doigt.

**Deux hypothèses initiales écartées après vérification du code** (documentées
ici pour que le travail ne soit pas refait) :
- `POST/PATCH/DELETE /employees/{id}/time-entries/start|stop` ressemblait de
  loin à un pointage en libre-service côté manager (donc une SAISIE mal
  gardée, comme `/planning/me/time-entries/start`). La lecture de
  `TimeEntryDetailSheet.tsx` montre que ces appels envoient un
  `clock_in_at`/`clock_out_at` explicite saisi dans un formulaire d'édition
  manager — c'est une correction de pointage, pas un pointage en direct.
  Classée CORRECTION, gardée à raison.
- Le reste des sous-ressources « lecture » (`weeks`, `shifts`,
  `shift-templates`, `week-templates`, `employees`) a été laissé côté
  CONFIGURATION/gardé plutôt que reclassé CONSULTATION libre : contrairement
  aux quatre référentiels ci-dessus, ces lectures exposent soit le planning
  brouillon/non publié de tout l'établissement (`weeks`, `shifts` — au-delà
  de ce que `/planning/me/team-week` publie déjà), soit des données RH
  (`employees`), et n'ont aucun cas d'usage identifié hors du poste
  manager qui construit déjà le planning. Pas assez de justification pour
  les faire basculer.

### 1.7 `/haccp` — flutter + back-office ⚠

| Méthode | Route(s) | Client(s) | Objet | Garde | Catégorie | ⚠ |
|---|---|---|---|---|---|---|
| GET | `/settings`, `/hub`, `/temperature-zones`, `/corrective-actions`, `/cleaning-zones`, `/cleaning-surfaces`, `/activities`, `/components` | flutter, back-office | Consultation config/référentiel HACCP | aucune | CONSULTATION | |
| POST | `/temperature-readings/batch` | flutter | Relever une série de températures | aucune | SAISIE | |
| POST | `/cleaning-sessions` | flutter | Logger un nettoyage effectué | aucune | SAISIE | |
| GET | `/cleaning-sessions/{id}`, `/temperature-sessions/{id}` | flutter, back-office | Consulter le détail d'une session | aucune | CONSULTATION | |
| POST | `/uploads/haccp` | flutter | Joindre une photo à un relevé | aucune | SAISIE | |
| POST, PATCH, DELETE | `/cleaning-zones*`, `/cleaning-surfaces*`, `/temperature-zones*` ; PUT `/settings` | back-office | Configurer zones, surfaces, seuils, réglages HACCP | aucune | CONFIGURATION *(non gardé — hors périmètre, voir §5)* | |
| **POST, GET** | **`/traceability`** | **flutter (créer + lister), back-office (lire)** | **Enregistrer/consulter un lot tracé (réception marchandise, photo + commentaire)** | **`haccp.manage`** | **SAISIE + CONSULTATION** | **⚠** |
| **GET** | **`/traceability/{id}`** | **flutter, back-office** | **Consulter le détail d'un enregistrement de traçabilité** | **`haccp.manage`** | **CONSULTATION** | **⚠** |

La garde sur `/haccp/traceability` est documentée dans `routes.go` comme une
décision assumée (« décision assumée pour ce nouveau module, voir la
conversation d'architecture HACCP traçabilité du 2026-07-23 »), pas un
oubli. Mais elle contredit directement le principe directeur du lot 7 :
c'est le seul sous-module HACCP où enregistrer et consulter un relevé est
gardé, alors que le relevé de température, le log de nettoyage et le upload
photo — des SAISIES strictement comparables — sont libres juste à côté.
**C'est l'incohérence la plus grave du lot** (voir résumé en tête de
document) : traçabilité = obligation légale quotidienne, appelée en
conditions réelles par l'app opérationnelle (flutter), et bloquée pour
n'importe quel rôle restreint sans `haccp.manage`.

### 1.8 `/customers` — back-office (+ recherche partagée flutter) ⚠ partiel

| Méthode | Route(s) | Client(s) | Objet | Garde | Catégorie | ⚠ |
|---|---|---|---|---|---|---|
| GET | `/search`, `/list` | back-office, flutter | Rechercher/lister les clients | aucune | CONSULTATION | |
| GET, POST, PATCH, DELETE | `/loyalty-programs*` | back-office | Gérer les programmes de fidélité | aucune | CONFIGURATION *(non gardé, hors périmètre)* | |
| GET, PATCH | `/{customer_id}/loyalty*`, `/{customer_id}/rewards/{reward_id}` | back-office | Consulter/ajuster la fidélité d'un client donné | aucune | CONSULTATION / CORRECTION légère | |
| POST | `/import/preview`, `/import/commit` ; GET `/import/template` | back-office | Importer des clients en masse | `customers.manage` | CONFIGURATION | |
| **POST** | **`/` (création unitaire)** | **back-office** | **Créer une fiche client (bouton « Créer un client »)** | **`customers.manage`** | **SAISIE** | **⚠** |

Le code (`cmd/api/routes.go:1230-1234`) explique la garde par un raisonnement
explicite : *« même garde RBAC que l'import, pour la même raison : ça écrit
dans le fichier client d'un marchand »*. C'est précisément le raisonnement
que le principe du lot 7 invalide — le fait qu'une action écrive des données
ne la rend pas CONFIGURATION (relever une température écrit aussi une
donnée). Créer une fiche client est une saisie courante — un serveur qui
inscrit un client au programme de fidélité en salle, ou ajoute ses
coordonnées, en a besoin au quotidien. Le domaine menu voisin applique
d'ailleurs la distinction correctement : `POST /menu/products` (créer un
produit) est libre, seul l'import en masse est gardé — `/customers` devrait
suivre la même ligne.

### 1.9 `/menu` — back-office (config) + flutter (saisie) — pas d'incohérence

Seules les 3 routes d'import (`/import/preview`, `/import/commit`,
`/import/template`) portent `catalog.manage` — décision déjà assumée et hors
périmètre de ce lot (précédent explicite cité dans la consigne). Tout le
reste — création/édition/suppression de produits, catégories, tags,
attributs, mises à jour en masse de prix/TVA/statuts (`back-office`, toutes
CONFIGURATION) et bascule de disponibilité produit/composant (« 86 » d'un
plat en rupture, `flutter`, SAISIE) — est `aucun`. Rien à signaler côté
sur-garde ; le sous-dosage (prix modifiables sans aucun droit) est noté en
§5, hors périmètre demandé.

### 1.10 `/orders`, `/bookings`, `/delivery_sessions`, `/cash_register`, `/stocks`, `/floors`, `/locations`, `/printers`, `/integrations`, `/pos` (hors statut) — pas d'incohérence

Aucune de ces routes ne porte de garde `permission.Key`, quel que soit le
client appelant. Impossible d'y trouver une CONSULTATION/SAISIE mal gardée
par construction. Inventaire complet (flutter : prise de commande complète,
réservations, tournées de livraison, caisse, imprimantes ; back-office :
mêmes domaines côté consultation/reporting + intégrations Uber Eats/
Deliveroo/Stripe/kiosque) fourni dans les tableaux ci-dessus par domaine —
pas reproduit ligne à ligne ici puisqu'aucune garde n'existe à auditer.
Notes de sous-dosage (accepter/refuser/rembourser une commande, clôturer une
caisse, etc. — tout CORRECTION/CONFIGURATION et pourtant libre) en §5.

### 1.11 `/pos/settings/kiosk` — back-office — pas d'incohérence

`GET .../devices/{id}/admin-pin` et `POST .../regenerate-admin-pin`
(consulter/régénérer le PIN admin d'une borne) portent `settings.manage` —
CONFIGURATION de sécurité, exactement le genre d'action qu'un restaurateur
voudrait réserver à un petit nombre de personnes. Bon exemple de garde déjà
correctement dimensionnée, comme `pos.status.manage`. Le reste du domaine
(lister/activer/désactiver/révoquer une borne, codes d'enrôlement, réglages
kiosque incl. médias) est `aucun` — sous-dosage, hors périmètre (§5).

---

## 4. Propositions

### 4.1 `haccp.manage` sur `/haccp/traceability` (POST, GET, GET `/{id}`)

**Recommandation : (a) retirer la garde.** Aligne la traçabilité sur le
reste du module HACCP (température, nettoyage, upload photo — tous SAISIE
libres). Test de la consigne appliqué : aucun restaurateur ne retirerait à
un employé de cuisine réel la capacité d'enregistrer une réception tracée —
c'est une obligation légale qu'il doit pouvoir remplir seul, comme il
remplit déjà ses relevés de température. **0 nouveau droit.**

### 4.2 `customers.manage` sur `POST /customers/`

**Recommandation : (a) retirer la garde.** Garder `customers.manage`
uniquement sur les 3 routes d'import en masse (déjà le cas), cohérent avec
le traitement du domaine menu (import gardé, création unitaire libre). Test
appliqué : aucun restaurateur ne retirerait à un serveur la capacité
d'ajouter un client au fichier lors d'une commande ou d'une inscription au
programme de fidélité. **0 nouveau droit.**

### 4.3 `staff.schedule.manage` sur `/planning` — 4 lectures de référentiel

**Recommandation : (b) déplacer la garde d'un cran.** Retirer le
`r.Use(middleware.RequirePermission(permission.StaffScheduleManage))` posé
en tête du groupe `/planning`, et le reposer individuellement sur toutes les
routes CONFIGURATION/CORRECTION listées dans le tableau du §1.6 (l'immense
majorité du groupe reste gardée, sans changement de comportement). Seules
`GET /planning/contract-types`, `GET /planning/attendance-sources`,
`GET /planning/event-types` et `GET /planning/positions` passent en libre
(authentifié, sans droit). Test appliqué : ce sont des listes de libellés
sans donnée sensible (vérifié dans le code Go) — aucun restaurateur n'a de
raison de les cacher à un employé. **0 nouveau droit** — c'est un
déplacement de garde existante, pas une création.

### Total

**0 droit ajouté au catalogue** sur les trois propositions — largement sous
le seuil de 3 fixé par la consigne, cohérent avec une application franche du
principe (a) : la grande majorité des cas se résolvent en retirant une garde
plutôt qu'en inventant un droit plus fin. Aucune des incohérences trouvées
ne correspond à un cas où un restaurateur voudrait réellement restreindre
l'opération à certains employés (test de la consigne appliqué à chacune) —
l'issue (c) n'a donc été retenue nulle part dans ce lot.

---

## 5. Observations hors périmètre (non demandées, notées pour un lot futur)

Le lot 7 ne demandait que les cas de **sur-garde** (CONSULTATION/SAISIE
gardée par `*.manage`). En parcourant l'inventaire, le motif inverse est
bien plus fréquent et plus net : des opérations clairement CONFIGURATION ou
CORRECTION, appelées par `back-office`, sans aucune garde RBAC — déjà
largement documenté dans le « Résumé chiffré » de `docs/RBAC_ROUTES.md`, mais
concrètement identifiable ici par client :
- **Menu** : modification/suppression de produit, mise à jour de prix en
  masse (`PATCH /menu/products/bulk`), TVA en masse — aucune garde.
- **HACCP** : définir les seuils de température, créer/éditer les zones et
  surfaces de nettoyage, paramétrer le module (`PUT /haccp/settings`) —
  aucune garde, alors que la lecture/écriture courante (relevés, nettoyages)
  est, elle, correctement libre.
- **Orders/Bookings/Cash register** : rembourser une commande, rouvrir un
  ticket, clôturer/enclore une caisse, annuler une réservation confirmée —
  aucune garde, alors que des droits dédiés existent déjà au catalogue et ne
  sont utilisés nulle part (`pos.ticket.reopen`, `pos.refund`,
  `pos.discount.apply`, `pos.cash_drawer.open`, `inventory.manage`,
  `reports.sales.read`, `reports.financial.read`).
- **Planning** : `GET /planning/performance` (coût de la main d'œuvre vs CA)
  reste sous `staff.schedule.manage` faute d'un droit `reports.*` mieux
  taillé — actuellement sans conséquence pratique (toujours gardé), mais à
  reconsidérer si `staff.schedule.manage` est un jour distribué plus
  largement qu'un rôle strictement managérial.

Ce sont des candidats naturels pour un lot 8 « sous-dosage », mais ils sont
explicitement hors du périmètre confié à ce lot 7 (audit de sur-garde
uniquement) et ne changent aucune garde ici.
