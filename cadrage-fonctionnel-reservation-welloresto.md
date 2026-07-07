# Module de Réservation WelloResto — Cadrage Fonctionnel

| | |
|---|---|
| **Objet** | Cadrage fonctionnel d'un module de réservation natif intégré à la caisse WelloResto |
| **Statut** | Cadrage prêt — à committer dans `ib-welloresto-api` |
| **Périmètre** | Fonctionnel uniquement (aucun choix technique ni design à ce stade) |
| **Version** | 0.6 |

> **Note de lecture.** Ce document fixe *le quoi* et *le pourquoi*. Le *comment* (architecture, modèle de données, endpoints, UI) fera l'objet d'une passe technico-fonctionnelle ultérieure. Les fonctionnalités sont priorisées en **MoSCoW** (Must / Should / Could / Won't) et regroupées en lots livrables.

---

## 1. Contexte & positionnement stratégique

Le marché de la réservation se divise en deux philosophies :

- **Les marketplaces** (TheFork, OpenTable) apportent une audience de clients en recherche, mais prélèvent une commission au couvert et conservent la propriété de la donnée client.
- **Les suites de réservation directe** (Zenchef, SevenRooms, Resy) laissent au restaurateur la propriété du client et de sa donnée, sans commission au couvert, mais n'apportent pas d'audience.

**WelloResto étant une caisse, le module se positionne dans le second camp — réservation directe, sans commission, données propriété du restaurateur.** L'avantage différenciant est que la réservation, la caisse, le paiement (Stripe Connect), le CRM et les canaux de commande (ScannOrder, kiosk, QR, click & collect) vivent dans **un seul système**. Là où un pure-player doit s'interfacer à une caisse par API et perd la profondeur temps réel, et où un POS concurrent a une réservation mais un CRM plus pauvre, WelloResto peut offrir une **intelligence client de niveau SevenRooms, nativement, au prix de l'indépendant français**.

---

## 2. Architecture d'exposition (principe directeur)

Le module suit le pattern déjà en place dans WelloResto pour la commande (`orders` staff / `scannorder` public) :

- **`/bookings` — API staff**, consommée par l'application **Flutter POS** : gestion opérationnelle du carnet, plan de salle, walk-ins, actions manuelles.
- **`/reservation` (routes `/rsv/{slug}`) — API publique**, consommée par l'**application web de réservation** destinée aux clients finaux.

Les deux couches partagent la **même logique métier** et le **même modèle de données**, avec droits d'accès et parcours différenciés.

**Principe transverse — tout est paramétrable côté back-office.** Aucune règle métier hardcodée.

**Principe de parité fonctionnelle staff/public.** Les fonctionnalités « intelligentes » (disponibilité optimisée, liste d'attente, communication automatique) sont disponibles des deux côtés.

---

## 3. Objectifs & indicateurs cibles

| Objectif | Indicateur | Cible indicative |
|---|---|---|
| Réduire les no-shows | Taux de no-show | < 5 % (réf. : jusqu'à ~20 % sans outil ; ~2,9 % chez les meilleurs) |
| Remplir les services creux | Taux de remplissage sur créneaux faibles | En hausse mesurable |
| Maximiser la marge | Part de réservations directes vs marketplace | Maximale (0 commission) |
| Sécuriser la venue | Taux de transformation résa → visite | > 80 % |
| Augmenter le panier | Valeur moyenne par couvert | En hausse via upsell contextuel |
| Fidéliser | Taux de ré-visite | En hausse (~+25 % observé avec un vrai CRM) |
| Enrichir la connaissance client | Fiches clients enrichies (préférences + dépenses) | Croissant |

---

## 4. Personas & besoins

| Persona | Canal | Besoin principal |
|---|---|---|
| **Restaurateur / gérant** | BO React | Paramétrer, remplir, réduire no-shows, fidéliser, piloter. |
| **Personnel de salle / hôte** | Flutter POS | Placer vite, visualiser les arrivées, reconnaître les habitués, gérer walk-ins. |
| **Client final** | App web publique | Réserver en quelques clics 24/7, être rassuré, être reconnu. |
| **Admin / support WelloResto** | BO (droits étendus) | Configuration avancée, supervision multi-établissement. |

---

## 5. Benchmark synthétique

| Solution | Positionnement | Modèle éco. | Force clé | Limite |
|---|---|---|---|---|
| **TheFork** | Marketplace n°1 Europe | Commission ~2 €/couvert | Suite anti no-show complète sans surcoût | Commission érode la marge |
| **OpenTable** | Plus grand réseau mondial | Abonnement + commission | Acquisition | Coûteux au volume |
| **SevenRooms** | Référence mondiale du CRM | ~499 $+/mois, 0 commission | Fiche client auto-enrichie par la caisse | Cher, pas d'audience |
| **Resy** | Chouchou gastronomie | Forfait fixe, 0 commission | Anti no-show ~2,9 % | Réseau concentré |
| **Tock** | Prépaiement / billetterie | Prépaiement | Résa = transaction | Usage de niche |
| **Zenchef** | Référence FR tout-en-un direct | Abonnement, 0 commission | Widget + prépaiement + plan de salle + marketing | Prix élevé PME |
| **POS-natifs** (Toast, TouchBistro, Lightspeed) | Résa intégrée dans la caisse | Selon POS | Tables temps réel + commandes + fidélité | CRM plus pauvre |
| **Eat App / Kouver / Covero** | Meilleur rapport qualité-prix | Freemium / fixe | Plan gratuit crédible, propriété données | Fonctions avancées limitées |

**Assemblage cible :** anti no-show de TheFork + CRM de SevenRooms + tout-en-un sans commission de Zenchef + logique transactionnelle de Tock — nativement intégrés à la caisse.

---

## 6. Piliers fonctionnels

Colonne **Exposition** : `POS` (staff Flutter), `Web` (client public), `BO` (back-office), ou combinaison.

### 6.1 Prise de réservation

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Widget 24/7 embarquable, personnalisable, multi-langue | Web | **Must** |
| Moteur de disponibilité (durée variable selon groupe, shifts, capacité par zone) | POS + Web | **Must** |
| Prise de résa manuelle par le staff | POS | **Must** |
| Acceptation automatique on/off | BO → POS + Web | **Must** |
| Confirmation immédiate (email + SMS selon paramétrage) | POS + Web | **Must** |
| Modification / annulation par le client depuis son espace | Web | **Must** |
| Modification / annulation par le staff | POS | **Must** |
| Groupes au-delà du max : redirection vers appel resto | Web | **Must** |
| Réservation via fiche Google (Reserve with Google) | Web | **Should** |
| Formulaire privatisation / événements | Web | **Could — reporté** |
| Réseaux sociaux, IA téléphonique | Web / POS | **Could** |

### 6.2 Plan de salle & attribution des tables

**Note structurante — deux phases distinctes.**

- **En Lot 1** : placement à table **manuel**. Le staff attribue une ou plusieurs tables au moment de l'acceptation ou après. Seul contrôle automatique : le **nombre de places disponibles pour la réservation** (capacité paramétrée). Warning BO si capacité paramétrée > capacité physique.
- **En chantier distinct post-Lot 1 (refonte plan de salle)** : contrôle table par table, combinaisons **inférées géométriquement** (adjacence sans obstacle), obstacles, formes de tables enrichies, placement auto à 100 %.

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Plan de salle temps réel synchronisé caisse | POS | **Must** |
| Attribution manuelle d'une ou plusieurs tables à la résa | POS | **Must** |
| Contrôle de capacité (couverts déjà réservés + nouveaux ≤ capacité de la plage) | POS + Web | **Must** |
| Warning BO si capacité paramétrée > capacité physique des tables | BO | **Must** |
| Durée de table variable selon taille de groupe, paramétrable | BO → POS + Web | **Must** |
| Liste d'attente intelligente (walk-in, SMS quand table libre) | POS + Web | **Should** |
| Paramétrage de la liste d'attente | BO | **Should** |
| Réattribution automatique en cas de no-show | POS | **Should** |
| Contrôle de disponibilité table par table | POS + Web | **Won't Lot 1 — chantier plan de salle** |
| Combinaisons de tables par inférence géométrique | POS + Web | **Won't Lot 1 — chantier plan de salle** |
| Placement automatique à table à 100 % | POS + Web | **Won't Lot 1 — chantier plan de salle** |
| Séquençage cuisine / lissage de la charge | POS | **Could** |

### 6.3 Anti no-show

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Empreinte bancaire à la résa (Stripe Connect) | Web (+ POS) | **Must** |
| Politique paramétrable : délai gratuit, montant retenu | BO | **Must** |
| Prélèvement manuel ou automatique selon règles | POS + BO | **Must** |
| Annulation manuelle de l'empreinte par le staff | POS | **Must** |
| Prépaiement / acompte déductible de l'addition | Web (+ POS) | **Should** |
| Reconfirmation automatique paramétrable | BO → POS + Web | **Should** |
| Fiabilité client + blacklist automatique | Interne | **Should — post autres lots, en interne** |
| Menu prépayé événements | Web | **Could** |
| Yield / acompte majoré | — | **Won't** |

### 6.4 Communication client

Principe : couverture maximale par défaut, paramétrage granulaire BO, contenus textuels par défaut (non éditables) au lancement. Email toujours activé (coût nul). SMS opt-in avec mention « peut entraîner une surfacturation ».

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Paramétrage activation / désactivation par type de message | BO | **Must** |
| Paramétrage par message : choix canaux (email obligatoire, SMS opt-in) | BO | **Must** |
| Contenus textuels par défaut fournis par WelloResto | — | **Must** |
| Confirmation immédiate | POS + Web | **Must** |
| Rappel programmable + demande de reconfirmation | POS + Web | **Must** |
| Notification modification / annulation | POS + Web | **Must** |
| Notification liste d'attente | POS + Web | **Should** |
| SMS bidirectionnel (confirmation / modif / annulation par retour SMS) | Web | **Should** |
| Message post-visite : remerciement + avis | Web | **Should** |
| Relance no-show | POS | **Should** |
| Templates éditables avec variables | BO | **Could — évolution** |
| Multi-langues, WhatsApp | BO / Web | **Could** |

### 6.5 CRM & fidélisation

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Fiche client créée automatiquement à la résa | POS + Web | **Must** |
| Fiche enrichie : allergies, table préférée, occasion, historique visites + dépenses (remontée caisse) | POS + BO | **Should** |
| Tags manuels (VIP, allergène, occasion) | POS + BO | **Should** |
| Reconnaissance client à l'arrivée | POS | **Should** |
| Collecte et agrégation d'avis | Web + BO | **Should** |
| Tags automatiques (comportement, dépenses) | POS + BO | **Could** |
| Campagnes marketing automatisées | BO | **Could** |
| Programme de fidélité unifié tous canaux | Tous | **Could** |

### 6.6 Pilotage & analytics

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Tableau de bord : résas du jour, remplissage, alertes | POS + BO | **Must** |
| Indicateurs : no-show, valeur par couvert, sources | BO | **Should** |
| Prévisionnel, performance par service, ROI campagnes | BO | **Could** |

### 6.7 Acquisition & extensions

| Fonctionnalité | Exposition | Priorité |
|---|---|---|
| Synchronisation fiche Google + réseaux sociaux | Web | **Should** |
| Pré-commande liée à ScannOrder | Web | **Could — anticipé** |
| Marketplace WelloResto inter-restaurants | Vision | **Won't (long terme)** |

---

## 7. Règles de gestion arbitrées

Tous les paramètres ci-dessous sont **configurables depuis le back-office**, sauf mention contraire.

| Paramètre | Décision |
|---|---|
| **Durée de table** | Variable selon taille du groupe (par tranche) + fallback. Buffer inclus dans la durée. |
| **Empreinte bancaire** | Activable, délai gratuit paramétrable, montant retenu paramétrable, prélèvement manuel/auto, annulable manuellement. |
| **Acompte / prépaiement** | Should, post-empreinte. |
| **Yield** | Abandonné. |
| **Seuil de no-show** | Non paramétrable, géré en interne, ajouté après les autres lots. |
| **Fenêtre de réservation** | Deux bornes : max (jours à l'avance) et min (délai de coupure). |
| **Overbooking** | Paramétrable en pourcentage. |
| **Groupes min / max** | Paramétrables. Au-delà du max : redirection appel resto. |
| **Communication canaux** | Email toujours activé. SMS opt-in + mention surfacturation. |
| **Communication contenus** | Par défaut au lancement. Édition = évolution. |
| **Acceptation automatique** | On/off. |
| **Contrôle de capacité** | Actif Lot 1 (capacité par plage). |
| **Warning capacité vs plan de salle** | Alerte BO si capacité > capacité physique. |
| **Contrôle table par table** | Reporté (chantier plan de salle). |
| **Combinaisons de tables** | Reportées et **inférées automatiquement** par la géométrie (adjacence sans obstacle). |
| **Placement automatique** | Reporté (chantier plan de salle). |
| **Rotation des tables** | 5° partout (POS aligné sur l'éditeur BO). |

---

## 8. Parcours utilisateurs clés

**Client — nominal (acceptation auto activée)**
1. Ouvre l'app web publique.
2. Choisit date / heure / couverts ; le moteur propose les créneaux disponibles.
3. Empreinte bancaire selon politique.
4. Résa acceptée automatiquement, confirmation immédiate (email + SMS si activé).
5. Staff attribue table(s) plus tard.
6. Rappel J-1 / H-2, reconfirmation/modif/annulation possible.
7. À l'arrivée : reconnaissance, table prête, historique visible côté serveur.
8. Service ; acompte éventuel déduit ; encaissement NF525.
9. Post-visite : remerciement + avis selon paramétrage.

**Client — acceptation auto désactivée**
1. Étapes 1-3 identiques.
2. Résa en **attente** ; accusé de demande envoyé.
3. Staff notifié, examine, accepte en attribuant les tables (ou refuse).
4. Client reçoit confirmation ou refus.

**Staff — no-show**
1. Résa marquée no-show.
2. Table(s) libérée(s), disponibles pour la liste d'attente.
3. Politique appliquée (empreinte manuelle ou auto).
4. (Ultérieur, interne) Compteur de fiabilité incrémenté.

**Restaurateur — configuration BO**
1. Zones, capacités, durées par tranche, shifts.
2. Politiques d'empreinte.
3. Fenêtre, overbooking, groupes min/max.
4. Acceptation auto.
5. Canaux et messages.
6. Liste d'attente.
7. Consultation carnet + indicateurs.

---

## 9. Intégration native à WelloResto

- **Caisse ↔ fiche client.** Historique de dépense et plats préférés visibles à l'arrivée. *Killer feature.*
- **Flux continu résa → table → commande → addition.** Zéro double saisie.
- **Paiement (Stripe Connect).** Empreinte, prélèvement, encaissement NF525.
- **Messagerie (Brevo).** Confirmations/rappels via l'infra existante. Endpoints `/rsv` déjà partiellement branchés — à confirmer dans le cadrage technique.
- **Moteur d'upsell IA.** Nourri par la fiche résa (occasion, profil).
- **ScannOrder.** Pré-commande à la résa, anticipée non livrée.
- **Kiosk / QR.** Walk-in via liste d'attente.
- **Fidélité unifiée** et **multi-établissement**.

### 9.1 Refonte du plan de salle (chantier distinct, post-Lot 1)

Le plan de salle actuel est réutilisé tel quel en Lot 1. La refonte, planifiée après le Lot 1, débloque les fonctionnalités avancées et rehausse l'expérience visuelle au niveau des meilleurs (SevenRooms, Zenchef, Resy).

**Périmètre de la refonte :**

- **Obstacles sur le plan de salle** — liste restreinte :
  - **Mur** (la colonne est un mur ponctuel = cas particulier)
  - **Bar** avec design adaptatif à la taille
  - **Escaliers**
  - **Porte** avec sens d'ouverture
  Modélisables dans l'éditeur BO, visibles sur le POS.
- **Formes de tables enrichies** : ronde, carrée, rectangulaire, ovale, avec le bon nombre de chaises affichées.
- **Sémantique de distance** sur le canvas (échelle physique).
- **Inférence géométrique automatique des combinaisons de tables** (deux tables adjacentes sans obstacle = combinables). Différenciateur fort.
- **Contrôle de disponibilité table par table** dans le moteur de réservation.
- **Placement automatique à 100 %** s'appuyant sur les combinaisons inférées, les attributs de table, les préférences client.
- **Attributs de tables** (PMR, terrasse, VIP, fenêtre).
- **Zones-conteneurs** : évolution des `floor_areas` décoratifs vers de vraies zones porteuses de règles (à confirmer selon usage réel).

---

## 10. Roadmap détaillée

Chaque lot livre à la fois l'API (staff `/bookings` + public `/reservation`), les écrans BO de paramétrage, l'intégration POS Flutter, et l'app web publique.

### Lot 0 — Cadrage

- ✅ **Audit du module de réservation existant** (`audit-reservation-existant.md`).
- ✅ **Audit des tables & du plan de salle** (`audit-tables-plan-de-salle.md`).
- ✅ **Mini-audit consommateurs** (`audit-patch-coordinates.md`) : verdict A — `PATCH /locations/{id}/coordinates` est un endpoint orphelin, à supprimer.
- ✅ **Cadrage fonctionnel** (ce document, à committer dans `ib-welloresto-api`).
- ⏭️ **Cadrage technico-fonctionnel** : endpoints cibles par module (`/bookings`, `/reservation`, `/bookings/settings`, `/locations`, `/floors`), modèle de données cible complet, table de comparaison avec l'existant (garder / refondre / créer / supprimer), découpage du Lot 1 en tickets estimables.

---

### Lot 1 — Socle réservation (Must)

**Phase 0 — Assainissement tables & plan de salle** *(pré-requis, environ 1 sprint)*

- Créer les **migrations SQL manquantes** pour `locations`, `floors`, `floor_areas` (héritage PHP).
- **Corriger `UpdateTableRequest`** côté Go : prendre en compte `seats` et `shape` actuellement ignorés.
- **Créer les endpoints `PATCH /floors/{id}` et `DELETE /floors/{id}`** attendus par le back-office React.
- **Supprimer `PATCH /locations/{id}/coordinates`** (endpoint orphelin, cf. audit) : contrôle préalable des logs d'accès prod sur 30 jours (middleware d'audit existant, coût nul), suppression de la route + handler + méthodes + DTO `UpdateLocationCoordinatesRequest`, test de non-régression sur le déplacement de table depuis l'éditeur BO (qui utilise `PATCH /locations/tables/{id}`).
- **Aligner la rotation à 5°** côté POS (aujourd'hui arrondi à 90°) sur ce que produit l'éditeur.
- **Ajouter le contrôle de conflit table × créneau** (deux résas acceptées sur la même table au même moment = interdit).
- Créer les migrations manquantes pour `bookings`, `booked_location`, `bookings_settings`, `hours_of_operation` (même héritage PHP).

**Phase 1 — Refonte de la logique de réservation**

- Corriger les **trois défauts bloquants du flux public** relevés par l'audit :
  - Génération et insertion du `booking_number` sur les résas publiques.
  - Découplage des chemins d'auth `/bookings` (staff) vs `/reservation` (public) — plus de `UserFromContext` sur les routes publiques.
  - Sécurisation du `customer_id` : création propre de la fiche client si téléphone inconnu, contrôle cross-tenant, rate-limit.
- **Unifier le moteur de disponibilité** (une seule source de vérité, appelable par `/bookings` et `/reservation`, calcul en UTC en base, conversion en timezone marchand en I/O).
- Implémenter les règles : durée variable par tranche + fallback, fenêtre min/max, overbooking en %, groupes min/max, contrôle de capacité par plage.
- Endpoint d'**attribution manuelle d'une ou plusieurs tables** à une réservation existante (pattern `booked_location`, aligné sur `order_location`).

**Phase 2 — Paramétrage back-office**

- CRUD des `bookings_settings` (aujourd'hui absent d'API d'administration).
- Écrans BO React pour :
  - Zones, capacités, durées de table par tranche.
  - Shifts / hours of operation.
  - Fenêtre de résa (min/max), overbooking (%), groupes (min/max).
  - Acceptation automatique on/off.
  - Warning capacité paramétrée vs capacité physique des tables.
  - Paramétrage des canaux et types de messages actifs.

**Phase 3 — POS Flutter (`/bookings`)**

- Vue carnet de résas (jour / semaine).
- Prise de résa manuelle par le staff.
- Acceptation / refus d'une résa en attente, avec attribution de table(s).
- Modification et annulation.
- Vue plan de salle synchronisée (statut des tables temps réel).
- Notifications (nouvelle demande de résa, modification client, etc.).

**Phase 4 — App web publique (`/reservation`)**

- Refonte / création de l'app web.
- Widget de disponibilité et prise de résa.
- Confirmation immédiate.
- Espace client : consulter, modifier, annuler sa résa.

**Phase 5 — Communication de base**

- Réutilisation de l'infra Brevo (endpoints `/rsv` existants à réactiver — cron actuellement désactivé, rappel stub).
- Envoi des messages activés dans le BO : confirmation, rappel, modification, annulation.
- Contenus textuels par défaut.

---

### Lot 2 — Anti no-show (Must)

- **Intégration Stripe empreinte bancaire** sur la résa (via Stripe Connect existant : `SetupIntent`, deferred capture).
- **Politique d'empreinte paramétrable BO** : délai d'annulation gratuit, montant retenu.
- **Prélèvement manuel** depuis le POS (bouton staff sur la fiche résa no-show).
- **Prélèvement automatique** selon règles paramétrées (job).
- **Annulation manuelle** de l'empreinte (staff).
- **Rappels programmables + demande de reconfirmation** avant service (paramétrable délai + canal).
- **Écrans BO** de paramétrage anti no-show.

---

### Lot 3 — Salle & temps réel (Should)

- **Liste d'attente intelligente** : inscription web + SMS quand table libre.
- **Paramétrage BO liste d'attente** (activation, capacité, délai max, canal).
- **SMS bidirectionnel** (webhook Brevo entrant + parsing intent).
- **Réattribution automatique** en cas de no-show.
- **Notifications temps réel** POS via WebSocket + FCM (nouvelle résa, walk-in en liste d'attente, etc.).

---

### Lot 4 — CRM & fidélisation (Should / Could)

- **Enrichissement fiche client** : allergies, table préférée, occasion, historique visites + dépenses (remontée caisse).
- **Tags manuels** (BO + POS).
- **Reconnaissance client à l'arrivée** sur POS (fiche visible côté serveur).
- **Collecte et agrégation d'avis** (post-visite → agrégation BO).
- **Tags automatiques** (comportement, panier).
- **Campagnes marketing automatisées** (anniversaire, réactivation, nouveaux menus) — extension du système existant.
- **Post-visite** : remerciement + avis paramétrable.
- **Fidélité unifiée** tous canaux (extension du système existant).

---

### Lot 5 — Acquisition & extensions (Could)

- **Google Reserve**.
- **Pré-commande ScannOrder** (couplage résa ↔ commande).
- **WhatsApp** comme canal supplémentaire.
- **Upsell IA nourri par la résa** (occasion, profil).
- **Analytics avancés** : taux no-show, valeur par couvert, sources, prévisionnel.
- **Seuil de fiabilité no-show interne** (non paramétrable).
- **Acompte / prépaiement** en plus de l'empreinte.
- **Formulaire privatisation / événements**.
- **Templates éditables** avec variables.

---

### Chantier distinct — Refonte plan de salle (post-Lot 1)

- **Éditeur BO refondu** : formes de tables enrichies (ronde, carrée, rectangle, ovale), chaises affichées, obstacles (mur, bar adaptatif, porte avec sens d'ouverture).
- **Sémantique de distance** sur le canvas (échelle physique, non plus purement virtuelle).
- **Attributs de tables** (PMR, terrasse, VIP, fenêtre) — configurables BO, exploitables par la logique de réservation.
- **Migration des `floor_areas`** vers de vraies zones-conteneurs porteuses de règles (à confirmer).
- **Rendu POS aligné** (obstacles, formes, chaises).
- **Algo d'inférence géométrique des combinaisons** : détection d'adjacence entre tables, prise en compte des obstacles, calcul de la capacité combinée.
- **Contrôle de disponibilité table par table** dans le moteur de résa.
- **Placement automatique à 100 %** basé sur combinaisons inférées + attributs + préférences client.

---

### Vision (Won't à court/moyen terme)

- **Marketplace WelloResto** inter-restaurants (« TheFork maison » sans commission pour les restaurateurs partenaires).

---

## 11. Risques identifiés

| Risque | Description | Mitigation |
|---|---|---|
| **NF525 — résolution dynamique du nom de table** | Le nom de table sur les commandes est résolu à la lecture. Un renommage réécrit visuellement l'historique. Défaut de traçabilité en cas d'audit NF525. | **Gardé en l'état pour l'instant.** À traiter en dette technique : soit figer le nom à la création de la commande, soit verrouiller le renommage dès qu'un historique existe. |
| **Écart capacité paramétrée vs physique** | Le contrôle de capacité en Lot 1 se fait sur `booking_capacity` par plage, pas sur les tables. | Warning BO au paramétrage. Contrôle table par table livré post-refonte plan de salle. |
| **Fiabilité de l'inférence géométrique** | La détection auto des combinaisons repose sur la qualité du plan de salle (obstacles bien placés, tables aux bonnes positions). | Refonte plan de salle avec sémantique de distance et obstacles modélisables. Fallback : attribution manuelle toujours possible. |
| **Cron rappels actuellement désactivé** | Le stub du rappel Brevo côté `/rsv` est vide et le cron global désactivé (audit initial). | Réactivation en Lot 1 Phase 5, avec tests d'intégration. |

---

## 12. Différenciation stratégique (synthèse)

> WelloResto peut offrir l'**intelligence client de niveau SevenRooms**, l'**anti no-show complet de TheFork** et le **tout-en-un sans commission de Zenchef**, mais **nativement intégrés à la caisse, au paiement et aux canaux de commande, avec un paramétrage complet côté restaurateur** — au prix de l'indépendant français. À moyen terme, l'**inférence géométrique automatique des combinaisons de tables** (avec obstacles modélisés) constituera un différenciateur technique fort par rapport aux systèmes classiques.

---

## Annexe — Glossaire

- **No-show** : réservation dont le client ne se présente pas.
- **Empreinte bancaire** : pré-autorisation carte sans débit, débitée en cas de no-show selon la politique.
- **Turn time / rotation** : durée d'occupation d'une table avant réattribution.
- **MoSCoW** : Must / Should / Could / Won't have.
- **Walk-in** : client se présentant sans réservation.
- **`/bookings`** : API staff (Flutter POS).
- **`/reservation`** : API publique (app web client).
- **Acceptation automatique** : résa validée sans intervention du staff.
- **Placement automatique à table** : attribution automatique d'une ou plusieurs tables précises (reporté).
- **Combinaison de tables** : ensemble de tables individuelles réunies pour un groupe qui dépasse la capacité d'une table seule.
- **Inférence géométrique** : détection automatique de combinaisons possibles via l'adjacence physique des tables et l'absence d'obstacle entre elles.
