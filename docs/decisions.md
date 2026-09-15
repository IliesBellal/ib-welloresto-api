### LOT B F1/F2 — Clôture : liste blanche de suspension, non-unification expires_at/trial_ends_at, correctif mandat actif (2026-09-14)

**F1 — liste blanche de suspension.** Ajouté à `internal/middleware/require_not_suspended.go`
exactement les quatre groupes identifiés en revue comme des consultations
bloquées par erreur (POST utilisé pour porter des critères de filtre, jamais
pour écrire) : `/v1/analytics` (préfixe — couvre ses 4 sous-groupes
`merchants`/`cancellations`/`clients`/`upsell`, tous enregistrés sous ce même
préfixe) ; et, en correspondance EXACTE (pas préfixe, pour ne pas exempter
les routes sœurs mutatives du même groupe) : `/v1/orders/pricing`,
`/v1/orders/upsell`, `/v1/orders/list`, `/v1/orders/history`,
`/v1/cash_register/history`, `/v1/bookings` et `/v1/bookings/` (racine —
`SearchBookings`). Vérifié explicitement que `/v1/orders/create`,
`/v1/bookings/create` restent bloqués. `/v1/orders/{id}/invoice/email-sms`
laissé bloqué **explicitement, pas par oubli** : ce n'est pas une
consultation, ça déclenche un envoi réel au client final du restaurant — un
effet externe incompatible avec la suspension. Test unitaire
(`require_not_suspended_test.go`) étendu avec les 4 groupes + les 3 cas
négatifs ci-dessus.

**F1 — bug pré-existant trouvé en marge (bien plus grave que ce que F1
demandait) : toute la liste d'exemptions, depuis son écriture d'origine en
B2b-2, portait un préfixe `/v1/` qui ne correspond à AUCUNE route réelle de
ce dépôt.** Découvert en tapant directement le serveur staging déployé pour
les besoins de F3 (`POST /admin/overrides` → 401, route existe ;
`POST /v1/admin/overrides` → 404, n'existe pas). `cmd/api/routes.go` ne
monte qu'un tout petit groupe (`/signup`, `/public`, `/auth/google`,
`/merchants/{id}/onboarding`) sous `r.Route("/v1", ...)` — `/admin`,
`/billing`, `/pos`, `/accounting`, `/analytics`, `/orders`, `/bookings`,
`/cash_register` sont tous montés directement à la racine du routeur.
Conséquence réelle : **cette liste d'exemptions n'a jamais correspondu à une
seule requête depuis son écriture** — un marchand `suspended` se voyait
bloqué même sur les exports fiscaux et la clôture de caisse, l'exact inverse
de ce que B2b-2 devait garantir (§7.5/§7.6). Corrigé : tous les préfixes/
chemins de `require_not_suspended.go` et de son test sont désormais sans
`/v1/`. Reconfirmé par le test unitaire (toujours vert) — la vérification en
conditions réelles contre un marchand `suspended` authentifié reste à faire
au prochain déploiement (voir F3, cette correction n'a pas encore été
redéployée au moment d'écrire ces lignes).

**F1 — `expires_at`/`trial_ends_at`, décision confirmée : ne pas unifier
maintenant.** Le chevauchement conceptuel entre les deux colonnes (déjà
signalé en B2c-1, voir ci-dessous) reste de la dette technique **explicite**,
pas un oubli. Ni `subscription_overrides.expires_at` (marqueur passif, aucun
comportement branché) ni `trial_ends_at` (comportement actif, mais seulement
pour `kind='price'`) ne sont touchés dans ce chantier. Reporté au lot C,
une fois toutes les dérogations réellement créées en production connues —
tenter une unification maintenant, sur la seule base d'une hypothèse de ce à
quoi ressembleront les dérogations réelles, risquerait de figer le mauvais
schéma.

**F2 — vérifié, bug trouvé et corrigé.** Scénario demandé : une dérogation
`price` LIVE via `trial_ends_at`, PUIS un vrai mandat SEPA accepté AVANT
l'échéance de la dérogation — la logique d'expiration doit vérifier un
mandat réellement actif, pas seulement une ligne `subscription_overrides`.

Ce que j'ai trouvé, avant correction : `Repository.HasSepaMandate`
(`internal/modules/subscriptions/overrides_repository.go`) ne vérifiait que
`SELECT EXISTS(SELECT 1 FROM sepa_mandates WHERE merchant_id = ?)` — aucun
filtre sur `sepa_mandates.status`. Ça fonctionnait aujourd'hui, mais **par
accident** : aucun code de ce dépôt n'écrit jamais un statut autre que
`'active'` (`billing.MandateStatusActive`) dans `sepa_mandates` — il n'existe
encore aucun flux de révocation/annulation de mandat. Le jour où une ligne
`sepa_mandates` avec un statut non-actif apparaîtrait (mandat révoqué côté
Stripe, par exemple), `RunTrialExpiryCheck` aurait laissé la dérogation
"s'éteindre en douceur" au lieu de correctement repasser le marchand en
SETUP.

Corrigé : `HasSepaMandate` filtre désormais explicitement
`AND status = 'active'`. Pas d'import du paquet `billing` pour réutiliser sa
constante (celui-ci importe déjà `subscriptions`, un import inverse
boclerait — même raison déjà documentée en B2c-0) ; le littéral `'active'`
est répété avec un commentaire renvoyant à `billing.MandateStatusActive`.

**Tests F2** (`overrides_trial_postgres_integration_test.go`), tous verts
contre staging :
- `TestRunTrialExpiryCheck_MandateAcceptedBeforeDeadline_Postgres` — le
  scénario exact demandé : dérogation créée avec une échéance future, mandat
  accepté en cours de trial (bien avant l'échéance), rien ne bouge tant que
  l'échéance n'est pas atteinte, puis à l'échéance la dérogation s'éteint et
  `activation_state` reste `LIVE` inchangé.
- `TestRunTrialExpiryCheck_InactiveMandateOnly_RevertsToSetup_Postgres` —
  reproduit la régression que l'ancienne requête ne pouvait pas attraper : une
  ligne `sepa_mandates` existe mais `status='canceled'` → doit repasser en
  SETUP. **Confirmé en désactivant temporairement le correctif** que ce test
  échoue sans lui (`activation_state` restait `LIVE` à tort), avant de le
  restaurer.

Suite complète (`subscriptions`/`billing`/`dunning`/`webhook/stripe`/
`cash_registers`/`middleware`) exécutée sans régression après F1/F2.

**F3 — exécuté contre le vrai serveur staging déployé, DEUX bugs réels
trouvés, un corrigé et reconfirmé, un fix en attente de redéploiement pour
vérification finale.**

Préalable confirmé avant tout test : deux webhook endpoints Stripe (mode
test) sont déjà configurés et `enabled`, pointant vers
`https://welloresto-api-staging.onrender.com/webhooks/stripe` (`GET
/v1/webhook_endpoints` — un endpoint porte sur son `application` un Connect
app id, l'autre est au niveau du compte, 238 événements activés dont
`invoice.created`/`invoice.paid`). Aucun tunnel nécessaire : le serveur
staging est déjà publiquement joignable. L'utilisateur a déployé le lot B ;
vérifié que le serveur répond désormais correctement aux vraies routes
(401 sur les routes protégées, plus de 404).

**Méthode.** Outil jetable `cmd/f3_webhook_verify` (non committé, supprimé en
fin de chantier) : crée un vrai marchand de staging, un vrai Customer/
PaymentMethod/SetupIntent Stripe (mode test, IBAN de succès FR officiel),
confirme le mandat, laisse Stripe lui-même déclencher et livrer les vrais
webhooks HTTP vers le serveur staging déployé (jamais un appel simulé),
sous une vraie Test Clock Stripe (méthode B2c-0). Vérifie la livraison via
`GET /v1/events/{id}.pending_webhooks` (0 = livré avec succès à tous les
endpoints) ET l'effet réel en base staging. Nettoyage systématique
(suppression de la Test Clock — cascade sur Customer/Subscription/factures/
moyen de paiement — puis des lignes DB) après chaque run, y compris les runs
avortés.

**Bug n°1 (artefact du script de test, pas du produit) : premier run en
échec avec `invoice error: "The customer does not have a payment method ...
must be attached to the customer"` sur `setup_intent.succeeded`.** Cause :
le script créait le Customer/SetupIntent directement via l'API Stripe sans
jamais passer par `POST /billing/sepa/setup`, donc sans ligne
`platform_billing_customers` préexistante. `HandleSetupIntentSucceeded` →
`CreateOrUpdateStripeSubscription` → `resolveOrCreateBillingCustomer` ne
trouvait donc aucune ligne, créait un **second** Customer Stripe sans
rapport, et tentait d'y attacher le moyen de paiement du premier — rejeté
par Stripe à raison. Corrigé côté script (pré-création de la ligne
`platform_billing_customers` avant confirmation du mandat, exactement ce que
`CreateSepaSetup` aurait fait). **Effet de bord découvert en nettoyant** :
Stripe a retenté le webhook 3 fois avant l'échec définitif, et
`HandleSetupIntentSucceeded` a inséré **3 lignes `sepa_mandates`
dupliquées** avant d'échouer à chaque tentative sur la création de
subscription — `CreateMandate` n'a aucune protection d'idempotence sur un
rejeu de webhook. Sans conséquence dans le flux réel normal (où ce chemin ne
tente jamais de créer une seconde subscription à cause du même merchant),
mais une fragilité réelle et non triviale à corriger : **signalé, pas
corrigé dans ce chantier** — un webhook Stripe peut être rejoué pour
n'importe quelle raison transitoire (5xx, timeout réseau...), et chaque
rejeu après un `CreateMandate` réussi mais un échec plus loin dans le
handler créerait une nouvelle ligne. À traiter au lot C.

**Run propre (après correctif du script) : `setup_intent.succeeded` confirmé
de bout en bout par le VRAI HTTP, pour la première fois.**
`pending_webhooks` 1→0, `merchant.activation_state=LIVE`,
`sepa_mandates_count=1`, `subscriptions.status=active`,
`stripe_subscription_id` renseigné — tout produit par le vrai webhook livré
par Stripe au serveur déployé, aucun appel de simulation.

**Bug n°2 (réel, dans le produit, trouvé par ce même run) : `invoice.created`
et `invoice.paid` sont livrés avec succès (`pending_webhooks=0`, 200 OK) mais
ne mettaient à jour STRICTEMENT RIEN en base.** Confirmé en récupérant le
payload brut de l'événement réel
(`GET /v1/events/{id}` côté Stripe) : `metadata: {}`,
`subscription: "sub_1UFhDQ..."` (référence nue). **Stripe ne recopie jamais
les métadonnées d'une Subscription sur les Invoices qu'elle génère** —
hypothèse implicite de `HandleInvoiceCreated`/`HandleInvoicePaid`/
`HandleInvoicePaymentFailed` depuis B2a-0, jamais vérifiée contre un vrai
webhook avant ce chantier (B2c-0 n'avait vérifié que la lecture directe de
l'Invoice via l'API, jamais son passage par ces trois handlers). Résultat
avant correctif : `subscriptions.current_period_end` n'était jamais mis à
jour par un vrai cycle de facturation, et surtout — **cascade B2b-1 cassée
en silence** : `invoice.paid` n'aurait jamais appelé `dunning.ClearDunning`,
et `invoice.payment_failed` n'aurait jamais appelé
`dunning.Service.HandlePaymentFailed` (même lecture `invoice.Metadata["merchant_id"]`,
même retour silencieux `nil` si vide). C'est exactement le risque que F3
existait pour couvrir, et B2b-1 le supposait déjà réglé.

Corrigé : `internal/webhook/stripe/service.go` gagne
`resolveInvoiceMerchantID` — lit `invoice.Metadata["merchant_id"]` en
premier (chemin rapide gratuit, gardé si Stripe change un jour ce
comportement), sinon résout via `invoice.Subscription.ID` (référence nue,
toujours présente même non-expansée — même mécanisme que la découverte
PaymentMethod de B2b-0) contre `subscriptions.stripe_subscription_id`,
nouvelle méthode `Repository.GetMerchantIDByStripeSubscriptionID`.
**`invoice.Customer.ID` a été délibérément écarté comme clé de corrélation**
malgré sa présence : un Customer Stripe peut être mutualisé entre plusieurs
`merchant_id` (B2a-1) alors que chaque marchand garde toujours sa PROPRE
Subscription Stripe distincte — `Customer.ID` serait ambigu dans ce cas,
`Subscription.ID` ne l'est jamais. `HandleInvoiceCreated`, `HandleInvoicePaid`
et `HandleInvoicePaymentFailed` utilisent désormais tous les trois cette
résolution commune.

**Tests** : `TestHandleInvoiceCreated_EmptyMetadata_ResolvesViaSubscriptionID_Postgres`
rejoue exactement la forme du payload réel confirmé (`metadata:{}`,
`subscription` en référence nue) contre `HandleInvoiceCreated` réel. **Confirmé
en désactivant temporairement le correctif** que ce test échoue sans lui
(`current_period_end` jamais écrit) avant de le restaurer.
`GetMerchantIDByStripeSubscriptionID` couvert directement dans
`TestStripeRepository_Postgres` (résolution, aucune correspondance, entrée
vide). Suite complète (`webhook/stripe`/`billing`/`subscriptions`/`dunning`/
`cash_registers`/`middleware`) verte après le correctif — seul
`TestLogger_Flush_Postgres` (`internal/middleware/request_logger`, un écart
de formatage JSON sans rapport) échoue, préexistant, aucun fichier de ce
paquet touché ici.

**F3 — VERT, confirmé après redéploiement.** L'utilisateur a redéployé le lot
B (correctifs F1/F2/F3 inclus) sur staging. Cycle de facturation réel rejoué
intégralement contre le serveur staging désormais à jour (marchand 961,
nettoyé) :
- `setup_intent.succeeded` : livré (`pending_webhooks` 1→0),
  `activation_state=LIVE`, `sepa_mandates_count=1`,
  `subscriptions.status=active`, `stripe_subscription_id` renseigné.
- `invoice.created` : livré (`pending_webhooks=0`), **et cette fois
  `subscriptions.current_period_end` est bien mis à jour**
  (`2026-09-14 23:31:26 +0200`, conforme au `period_end` réel de la facture)
  — la preuve directe que le correctif `resolveInvoiceMerchantID` fonctionne
  contre un vrai webhook HTTP livré par Stripe au serveur déployé, pas
  seulement en test d'intégration local.
- Facture réglée en temps réel (mandat SEPA test, règlement asynchrone
  résolu en quelques minutes, pas de Test Clock avancée pour ce prélèvement) ;
  `invoice.paid` livré (`pending_webhooks=0`).

Nettoyage Stripe (Test Clock supprimée, cascade Customer/Subscription/
factures/moyen de paiement) et base (merchant 961 et lignes associées)
effectué. Outil jetable `cmd/f3_webhook_verify` supprimé du dépôt après ce
run — son rôle s'arrête ici, il n'a jamais été destiné à être committé.

**Restent ouverts pour un lot ultérieur (signalés, pas corrigés ici, hors
périmètre strict de F3)** :
1. Idempotence de `HandleSetupIntentSucceeded` sur un webhook rejoué
   (`CreateMandate` peut dupliquer une ligne `sepa_mandates` si un rejeu
   survient après elle mais avant la fin du handler — trouvé en marge,
   détail plus haut).
2. La vérification "en conditions réelles contre un marchand `suspended`
   authentifié" du correctif de préfixe `/v1/` (F1) n'a pas été rejouée via
   une vraie requête HTTP suspendue — seul le test unitaire pur
   (`isSuspendedReadOnlyExempt`) et l'inspection directe des routes montées
   ont confirmé la correspondance de chemin. Recommandé avant production :
   un marchand de test réellement `suspended` frappant une route exemptée et
   une route non-exemptée.

---

### LOT B F4 — Bilan de fermeture (2026-09-14)

**Fonctionnellement complet au sens du document (§7) : oui**, pour le
mécanisme décrit — dérogations (§7.1), aperçu de changement, mandat SEPA
(§7.4/§11.5.4), abonnement Stripe récurrent, cascade d'impayé et effets de
suspension (§7.5/§7.6), bandeau, prix de remplacement à échéance. Après
F1-F3, ce n'est plus seulement vérifié en tests d'intégration : le mandat
SEPA, l'abonnement récurrent et la facturation réelle ont chacun été
confirmés au moins une fois de bout en bout contre la vraie API Stripe test
ET le vrai webhook HTTP livré au serveur staging déployé.

**Limite fonctionnelle réelle, pas une approximation : la facturation
annuelle (`billing_cycle='annual'`) n'a AUCUN abonnement Stripe récurrent
réel possible aujourd'hui** — `ResolveStripeLineItems` la refuse
explicitement (`ErrAnnualStripeSubscriptionNotSupported`, B2c-0) faute de
correspondance Stripe pour la règle "×10 mois" du B1c. Un marchand annuel
n'a donc aucune des mécaniques B2 (mandat → abonnement → factures → cascade)
qui fonctionne réellement pour lui tant que ce n'est pas traité séparément.

**Approximations assumées, listées explicitement (pas des oublis)** :
1. **P2 — "propriétaire"** reste approximé par `users_rights.admin=TRUE` le
   plus ancien. Cas observé (`user_id=2`, 4 marchands) cohérent avec un
   compte plateforme/support, jamais confirmé comme une vraie franchise —
   jamais tranché autrement (B1d).
2. **Prorata de changement de composition** (B1e) : mois nominal 30 jours
   tant que `current_period_end` est `NULL` (marchand sans mandat réel).
3. **Comparaison pack vs à la carte (§7.7)** : deux conventions annuelles
   distinctes et non réconciliées (`pricing_catalog.annual_price_cents` vs
   règle "×10" de ce module) — la comparaison tourne toujours en mensuel
   pour éviter de les mélanger, jamais unifiée.
4. **Révocation d'une dérogation** (B1d) ne défait jamais l'effet
   (`*_enabled`/`override_price_cents`/`max_kiosks` non restaurés) — remise
   en état manuelle par le staff, aucune valeur "avant" tracée.
5. **`expires_at`/`trial_ends_at`** : chevauchement conceptuel non unifié,
   décision explicite (F1) de reporter au lot C.
6. **`marketplaces`** comme cible de dérogation `module` : rejeté
   (`ErrOverrideTargetUnsupported`, aucune colonne `*_enabled` dédiée),
   jamais résolu autrement.
7. **Idempotence de `HandleSetupIntentSucceeded`** sur un webhook rejoué :
   peut dupliquer une ligne `sepa_mandates` (trouvé en marge de F3, non
   corrigé, signalé pour le lot C).
8. **Mutualisation de Customer Stripe** (B2a-1) : le mécanisme existe et est
   couvert par des tests service/repository (`fakeStripe`), mais n'a jamais
   été exercé de bout en bout avec deux vrais marchands partageant un
   customer réel et chacun sa propre Subscription Stripe.
9. **Mélange live/test mode signalé sans être corrigé** :
   `packages.stripe_price_id` (l'ancien mécanisme, `packages.id=1`) pointe
   vers un objet Stripe en **mode LIVE** alors que ce dépôt tourne en test —
   sans rapport direct avec `pricing_catalog.stripe_price_id` (le nouveau
   mécanisme, B2c-0, sain), mais un signal de données de staging à nettoyer.
10. **Correctif de préfixe `/v1/`** (F1/F3) : revérifié par test unitaire et
    inspection directe des routes montées, jamais rejoué via une vraie
    requête HTTP contre un marchand `suspended` authentifié — recommandé
    avant production.

**Templates Brevo manquants qui bloquent le CONTENU des emails (logique déjà
vérifiée, seul le rendu Brevo manque)** :
- `trial_expiry_reminder.html` (B2c-1, rappels J-7/J-1)
- `dunning_first_notice.html`, `dunning_second_notice.html`,
  `dunning_weekly_reminder.html`, `dunning_final_notice.html` (B2b-1, cascade
  d'impayé)

Aucun outil de ce dépôt ne peut les créer côté Brevo — provisionnement
externe requis avant qu'un envoi réel produise un contenu non vide.

**Migrations LOT B appliquées à staging, vérifiées en direct sur le schéma
(pas seulement supposées depuis `migrations/todo/`, voir
[[project_migrations_todo_done_unreliable]])** — les 8 confirmées présentes
(`to_regclass`/`information_schema` interrogés directement, 2026-09-14) :

| # | Fichier | Objet vérifié en direct |
|---|---|---|
| 137 | `subscription_items` | table présente |
| 138 | `users_platform_staff` | `users.is_platform_staff` présente |
| 139 | `subscription_overrides` | table présente |
| 140 | `platform_billing_customers` | table présente |
| 141 | `sepa_mandates` | table présente |
| 142 | `subscription_dunning` | table présente |
| 143 | `pricing_catalog_stripe_price_id` | `pricing_catalog.stripe_price_id` présente |
| 144 | `subscription_overrides_trial` | `subscription_overrides.trial_ends_at` présente |

Toutes additives (`CREATE TABLE`/`ADD COLUMN IF NOT EXISTS`), rien à
retirer ; c'est la liste exacte à rejouer en production le moment venu, dans
cet ordre (dépendances : 139 avant 144 ; 140 avant 141).

**État réel des données à date (staging, 2026-09-14, hors données de test
créées et nettoyées par cette session)** : `users.is_platform_staff=true`
→ **0 compte** (les endpoints `/admin/overrides` restent inutilisables par
quiconque tant qu'un `UPDATE` manuel n'a pas été fait, question ouverte
depuis B1d jamais résolue) ; `subscription_overrides`, `sepa_mandates`,
`platform_billing_customers`, `subscription_dunning` → **0 ligne réelle**.
Tout le lot B a été vérifié par des marchands de test créés et nettoyés à
chaque chantier — aucun marchand réel n'a encore traversé ce mécanisme.

---

### LOT B B2c-1 — Prix de remplacement à échéance (2026-09-14)

**Chevauchement trouvé avant tout code, signalé plutôt que masqué** :
`subscription_overrides.expires_at` existe déjà depuis B1d (migration 139) —
un marqueur passif ("l'override n'est plus en vigueur"), sans AUCUN
comportement actif branché dessus (juste un filtre dans
`ListActiveOverrides`). `trial_ends_at` (ce chantier) est explicitement une
NOUVELLE colonne demandée par le brief, avec un vrai comportement actif
(rappels, bandeau, réversion). Implémenté tel que demandé (colonne
distincte), mais les deux colonnes se chevauchent conceptuellement —
`expires_at` reste du poids mort. À unifier dans un futur nettoyage, pas
dans ce chantier.

**Interprétation retenue, à confirmer** : le brief décrit le mécanisme
"retombe en SETUP" uniquement en termes de `activation_state` — un concept
qui n'a de sens que pour une dérogation `kind='price'` (celle qui, avec un
trial, doit VISIBLEMENT faire passer le marchand en LIVE dès la création,
sinon "il retombe en SETUP" à l'échéance n'aurait rien à quoi revenir).
`trial_ends_at` reste une colonne générique (n'importe quel kind peut la
porter, rappels/bandeau s'appliquent alors génériquement), mais seul
`kind='price'` déclenche l'octroi/la réversion de `activation_state` à la
création/l'échéance. Les dérogations `module`/`kiosk_quota` avec
`trial_ends_at` sont juste révoquées à l'échéance, sans tenter de deviner un
retour en arrière sur leur propre effet (même logique que la révocation
manuelle déjà documentée en B1d : aucune valeur "avant" n'est tracée nulle
part).

**Implémenté** : `subscriptions.Service.CreateOverride` accepte
`trialEndsAt` ; pour `kind='price'` avec une échéance, octroie LIVE
immédiatement (même mécanisme que B2a-3 : `went_live_at` posé une seule
fois, jamais écrasé). `RunTrialExpiryCheck` (appelé depuis le même créneau
`@hourly` que la cascade d'impayé B2b-1 — deux `add("@hourly", ...)`
séparés, pas un enregistrement cron distinct, pour garder l'isolation
`SkipIfStillRunning` par tâche) : rappels J-7/J-1 (jamais renvoyés, colonnes
`trial_reminder_*_sent_at`), et à l'échéance — `sepa_mandates` existant ?
override simplement révoqué (l'abonnement récurrent B2c-0 prend le relais) ;
sinon révoqué ET (kind='price' seulement) `activation_state` repasse à
`SETUP`, jamais `SUSPENDED` (comme demandé). Bandeau : `GET /v1/merchant/activation-status`
(B2b-3) porte désormais aussi `trial_ends_at` quand pertinent — distinct du
bandeau SETUP par construction (un marchand en trial est déjà `LIVE`, donc
la condition existante `activation_state == 'SETUP'` ne peut pas se
déclencher pour lui).

**Nouveau template Brevo requis, pas encore créé** : `trial_expiry_reminder.html`
(même caveat que les templates de la cascade d'impayé, B2b-1).

**Tests** : 6 tests d'intégration Postgres — octroi LIVE à la création (avec
et sans trial), rappels J-7 puis J-1 sans double-envoi, échéance sans mandat
(révocation + retour SETUP + subscriptions.status='setup'), échéance avec
mandat (révocation seule, LIVE inchangé). Tous verts contre staging, plus la
suite complète (`subscriptions`/`billing`/`dunning`/`webhook/stripe`/
`cash_registers`) sans régression.

---

### LOT B B2c-0 — Abonnement Stripe récurrent : préalable comblé, deux pièges réels trouvés (2026-09-13)

Rapporté isolément, avant tout code B2c-1/2, comme demandé — c'est le point
qui rendait toute la cascade B2b inerte.

**Constat de départ, avant tout code** : `packages.stripe_price_id`
("Essentiel", packages.id=1) — la seule ligne réellement peuplée d'un id
Stripe dans ce dépôt avant ce chantier — s'est avéré être un objet **LIVE
MODE** lors d'une tentative de lecture avec la clé test (`404 : "a similar
object exists in live mode"`). Inutilisable pour la vérification demandée, et
un signal de mélange live/test dans les données de staging sans rapport
avec ce chantier — signalé, pas corrigé ici (hors périmètre).

**Construit** :
- `pricing_catalog.stripe_price_id`/`per_unit_stripe_price_id` (migration
  143) — pricing_catalog porte désormais ses propres Price Stripe (plan ET
  module, contrairement à `packages` qui ne couvrait que les plans).
- `cmd/ensure_stripe_prices` : outil idempotent qui crée un Price Stripe
  réel (mode test) pour chaque code facturable sans en avoir encore un.
  Exécuté contre staging avec la clé test fournie — **9 Price créés**,
  documentés ici : essentiel (`price_1UF1QMIpOVvvxHBEiTfqWwrk`), pro
  (`price_1UF1QMIpOVvvxHBETZTXaesA`), complet
  (`price_1UF1QLIpOVvvxHBEuAehWMoP`), reservation
  (`price_1UF1QNIpOVvvxHBEAqRNuLVX`), haccp
  (`price_1UF1QNIpOVvvxHBEHnf9G7FI`), planning (flat,
  `price_1UF1QNIpOVvvxHBEl2GeFPQf`), planning per-employee
  (`price_1UF1QOIpOVvvxHBEyrp6oa9N`), marketplaces
  (`price_1UF1QOIpOVvvxHBExdwtMl2N`), delivery
  (`price_1UF1QMIpOVvvxHBEpqw9Dzow`), extra_seat
  (`price_1UF1QOIpOVvvxHBECz7nVOlN`). Ré-exécuté une 2e fois : confirmé
  idempotent (aucun doublon). kiosk/sms exclus, sans prix du tout (P1/P3).
- `subscriptions.Service.ResolveStripeLineItems` : résout les
  subscription_items actifs en vraies paires Price/quantité (miroir de
  `computeAmount`, mais Price Stripe au lieu de centimes) — échoue
  explicitement (`ErrStripeCatalogPriceMissing`) plutôt que d'improviser un
  montant, et refuse `billing_cycle='annual'` explicitement
  (`ErrAnnualStripeSubscriptionNotSupported` — aucune correspondance Stripe
  pour la règle "×10 mois" du B1c, hors périmètre de ce chantier).
- `billing.Service.CreateOrUpdateStripeSubscription` : crée l'abonnement
  Stripe au moment du mandat (`HandleSetupIntentSucceeded`), ou met à jour
  l'existant (`SyncSubscriptionItems`, diff par le code en métadonnée de
  chaque item Stripe) — ne crée jamais un second abonnement. Branché en
  retour dans `subscriptions.ApplyItemChanges` (B1e) via une petite
  interface (`subscriptions.StripeSyncer`), pas un import du paquet
  `billing` (qui, lui, importe déjà `subscriptions` pour résoudre les
  lignes — un import dans les deux sens aurait été un vrai cycle).

**Deux pièges réels trouvés en vérifiant contre la vraie API Stripe (mode
test, clé fournie), avant tout déploiement** :
1. **Un `PaymentMethod` simplement attaché à un Customer n'est PAS un
   mandat SEPA.** Sans un `SetupIntent` confirmé avec `mandate_data`
   (exactement le flux réel de B2a-3), l'abonnement Stripe créé reste
   `incomplete` indéfiniment — Stripe ne tente même pas le prélèvement
   automatique. Ce chantier utilise donc systématiquement le même chemin de
   confirmation que B2a-3 pour tout test, jamais un simple `Attach`.
2. **Un abonnement `incomplete` expire après 23h RÉELLES** (`incomplete_expired`,
   terminal, irréversible) — indépendant de l'avancement d'une Test Clock.
   La 1re tentative de vérification a fait avancer la Test Clock de 3 jours
   immédiatement après création, ce qui a fait expirer l'abonnement avant
   que le règlement SEPA asynchrone (qui, lui, se résout en temps réel,
   pas en temps simulé) ait pu aboutir. Corrigé : interroger en temps réel
   (pas via la Test Clock) jusqu'à ce que le 1er prélèvement se résolve,
   puis seulement ensuite avancer la Test Clock pour tester les cycles
   suivants.

**Preuve de bout en bout, contre un vrai marchand de staging (créé et
nettoyé) et la vraie API Stripe test, mandat réellement accepté** :
1. `CreateOrUpdateStripeSubscription` (1er appel) → Subscription Stripe
   réelle créée, statut `active` une fois le mandat confirmé, 1 ligne
   (essentiel).
2. `subscriptions.ApplyItemChanges(add=haccp)` → **le même** id
   d'abonnement Stripe (vérifié égal), passé à 2 lignes — jamais un second
   abonnement créé.
3. Test Clock avancée d'un cycle de facturation → **2 factures réelles**
   retrouvées pour ce Customer, montants cohérents avec l'évolution de la
   composition (7900 payée avant le changement de module, la suivante
   couvrant essentiel+haccp) — `invoice.created` se déclenche bien avec la
   composition réelle, pas un montant recalculé à la main.

**Non re-vérifié dans cette passe** : le webhook `invoice.created`/
`invoice.paid` lui-même n'a pas été rejoué littéralement depuis cette
vérification (pas de tunnel HTTP configuré) — seule la génération réelle
des factures par Stripe a été confirmée par lecture directe de l'API. Le
risque de forme (payload webhook vs objet API) déjà couvert pour
`setup_intent.succeeded` (B2b-0) est structurellement plus faible ici :
`Invoice.Metadata`/`PeriodEnd` sont des champs inline, pas des références
imbriquées expansibles — la classe de bug trouvée en B2b-0 ne s'applique
pas de la même façon.

**Tests automatisés** (sans Stripe réel, `fakeStripe` — cohérent avec le
reste de ce chantier) : résolution des lignes Stripe (nominal, code non
facturable, cycle annuel refusé, quantités metered), et
`CreateOrUpdateStripeSubscription` appelé 3 fois de suite ne crée qu'un seul
abonnement (1 `CreateSubscription`, 2 `SyncSubscriptionItems`). Tous verts
contre staging, plus la suite complète (`billing`/`subscriptions`/
`webhook/stripe`/`dunning`/`cash_registers`) sans régression.

**Résultat : B2c-0 est vert, la cascade B2b peut réellement se déclencher
de bout en bout.** Prêt à enchaîner sur B2c-1/B2c-2.

---

### LOT B B2b-1/2/3 — Cascade d'impayé, bandeau, effets de suspension (2026-09-12)

**B2b-1 — machine à états.** Nouveau module `internal/modules/dunning` +
table `subscription_dunning` (migration 142, appliquée à staging) : une
ligne = une cascade en cours pour un merchant, supprimée (pas juste
réinitialisée) au retour à `active` — "aucune ligne" est la seule source de
vérité pour "rien en cours" côté cron. Webhooks
`invoice.payment_failed`/`invoice.paid` étendus dans
`internal/webhook/stripe/service.go` (`invoice.paid` appelle désormais aussi
`dunning.Service.ClearDunning`, en plus de son écriture `subscriptions.status`
déjà en place depuis B2a-0). Cron `RunDunningCascade` (`@hourly`,
`cmd/api/tasks.go`) : ré-évalue l'état réel de chaque merchant `past_due` à
chaque exécution — jamais un envoi planifié à l'avance, exactement la
consigne du brief. Gating "aucun envoi pendant les services" implémenté en
réutilisant `internal/modules/openinghours` (déjà utilisé pour le statut
POS), pas réinventé.

**Bug trouvé et corrigé pendant l'écriture des tests** (pas en prod, avant
tout déploiement) : le lendemain de l'envoi du dernier rappel (48h avant
suspension), le cron retombait dans la relance hebdomadaire générique au
lieu de rester silencieux jusqu'à la suspension elle-même — `FinalNoticeSentAt`
déjà posé désactivait la branche "dernier rappel" sans empêcher la branche
suivante de s'exécuter. Corrigé : une fois dans la fenêtre des 48h, plus
aucune autre branche ne s'exécute avant la suspension. Test qui l'a
attrapé : `TestRunCascade_FinalNoticeAndWeeklyReminder_Postgres` (2 exécutions
successives du cron, la 2e ne doit rien renvoyer).

**"Réessayer maintenant"** (`POST /v1/billing/retry-now`) : retente la
collecte sur la dernière facture Stripe ouverte du Customer déjà attaché
(`Invoices.List(status=open)` + `Invoices.Pay`), jamais une nouvelle saisie
d'IBAN. **Hypothèse explicite, à confirmer** : ce chantier suppose qu'un
mécanisme Stripe de facturation récurrente (Subscription + Invoices réels)
existe déjà en amont de cette cascade — B2a/B2b ne construisent que la
réaction aux événements `invoice.*`/`setup_intent.*`, jamais la création de
la Subscription/du prix récurrent elle-même. Si ce mécanisme n'existe pas
encore réellement, `invoice.payment_failed`/`invoice.paid` ne se
déclencheront jamais et toute la cascade reste inerte en pratique — à
vérifier avant de considérer B2b comme utilisable de bout en bout.

**Nouveaux templates Brevo requis, pas encore créés** :
`dunning_first_notice.html`, `dunning_second_notice.html`,
`dunning_weekly_reminder.html`, `dunning_final_notice.html`. Le code est
complet et correct (`mailer.Service.SendAsync` est appelé avec les bons
noms/données) mais ces templates n'existent pas encore côté Brevo — aucun
outil de ce dépôt ne peut les créer. Un envoi réel restera vide/en erreur
tant qu'ils ne sont pas provisionnés côté Brevo.

**B2b-2 — effets de la suspension.**
- *Lecture seule back-office* : nouveau middleware
  `middleware.RequireNotSuspended`, branché juste après `authMiddleware`
  dans les ~40 groupes de routes authentifiées de `cmd/api/routes.go`
  (modification mécanique, `sed` sur le motif exact `r.Use(authMiddleware)` —
  vérifié qu'aucune occurrence n'a été manquée). Bloque toute requête non-GET
  d'un marchand `suspended`, sauf une liste d'exemptions explicite
  (`/admin`, `/billing`, `/pos/reports`, `/pos/accounting`, `/accounting`,
  `/cash_register/.../close` et `/enclose`). **Choix assumé, pas une revue
  exhaustive de chaque route de l'API** : cette liste vient de ce qui est
  explicitement nommé dans le brief (exports fiscaux, clôture de caisse) plus
  ce qu'un marchand suspended doit garder pour se régulariser (billing) et ce
  que le staff interne doit garder pour le faire à sa place (admin). D'autres
  routes mériteraient peut-être une exemption (rattachement/détachement
  d'appareil, gestion des accès utilisateurs pour corriger qui est
  propriétaire...) — à revoir avant de considérer cette liste comme
  définitive. Lecture toujours fraîche (jamais via l'utilisateur
  authentifié mis en cache Redis, `models.UserCacheTTL` = 60 minutes) —
  même raisonnement que pour le bandeau (B2b-3).
- *Canaux en ligne coupés* : `scannorder.Service.CreateOrderSNO` refuse
  désormais toute commande (Scan&Order et "QR à table", qui partagent ce
  même point d'entrée — `orderType == "IN"`) pour un merchant `suspended`,
  avant toute autre logique.
- *POS — refus d'ouverture de registre* :
  `cash_registers.Service.OpenCashRegister` refuse si
  `activation_state != 'LIVE'` OU `status = 'suspended'`, message unique
  "Votre caisse n'est pas encore activée. Rendez-vous dans votre espace de
  gestion." dans les deux cas — aucun montant, aucun détail. Vérifié que
  `past_due` seul (sans suspension) n'empêche PAS l'ouverture — seule la
  suspension effective bloque, conformément au §7.5/§7.6.
- *Exports fiscaux et clôture de caisse* : jamais bloqués (exemptés du
  middleware ci-dessus) — vérifié par test que ces chemins restent exempts.

**B2b-3 — bandeau et cache.**
`GET /v1/merchant/activation-status` (nouveau, authentifié seul) : lecture
toujours fraîche de `merchant.activation_state`/`subscriptions.status` —
**jamais** ajoutée à l'objet utilisateur authentifié mis en cache Redis
(`internal/modules/auth/service.go`, `models.UserCacheTTL` = 60 minutes).
C'est le point précis que le brief demandait de vérifier : ajouter ces deux
champs à l'objet caché aurait réintroduit jusqu'à 60 minutes de délai avant
la disparition du bandeau après le webhook `setup_intent.succeeded` — évité
en construction, pas par une invalidation de cache (aucun index
merchant→tokens n'existe pour invalider sélectivement les entrées Redis
d'un marchand). **Le back-office (dépôt séparé, hors périmètre de cette
session) doit interroger CET endpoint pour le bandeau, pas les champs
`activation_state`/abonnement déjà présents dans la réponse de login (qui,
eux, restent cachés)** — point d'intégration à transmettre.

**Tests** : 6 nouveaux tests d'intégration Postgres pour `dunning` (1er/2e/3e
échec, remise à zéro sur paiement réussi, suspension après échéance, dernier
rappel + relance hebdomadaire sans double-envoi, "réessayer maintenant"), 1
test unitaire pur pour la liste d'exemptions du middleware, 1 test
d'intégration pour `IsActivatedForOrdering` (les 4 combinaisons
SETUP/LIVE × suspended/actif/past_due), 1 test d'intégration pour le
bandeau. Tous verts contre staging. `go test ./...` ne montre que les
échecs préexistants déjà signalés (planning/employees, planning/leave,
planning/swaps, ubereats) — aucun fichier de ces paquets touché ici.

**Questions ouvertes** :
1. La liste d'exemptions du middleware lecture-seule doit être revue avant
   production — pas garantie exhaustive.
2. "Réessayer maintenant" suppose une vraie Subscription Stripe existante —
   à vérifier que ce mécanisme est bien en place, sinon toute la cascade
   reste inerte en pratique.
3. Templates Brevo à créer avant que les relances envoient un contenu réel.
4. Le back-office doit être informé de basculer sur
   `GET /v1/merchant/activation-status` pour le bandeau plutôt que sur les
   champs déjà présents (et cachés) de la réponse de login.

---

### LOT B B2b-0 — Vérification Stripe réelle : un bug trouvé et corrigé (2026-09-12)

Fait avant tout code de cascade, comme demandé. `STRIPE_API_KEY` fournie par
l'utilisateur pour cette vérification (clé test, communiquée hors fichier,
jamais écrite sur disque ni committée — l'utilisateur prévoit de la révoquer
après ce chantier).

**Méthode.** Plutôt que de configurer un tunnel/Stripe CLI pour recevoir un
vrai webhook HTTP (complexité opérationnelle sans plus-value ici — la
vérification de signature est de toute façon différée, voir B2b-2 du brief
d'origine, donc `internal/webhook/stripe/http_handler.go` n'exige aucun
secret de webhook et accepte tout corps JSON), vérification directe contre
la vraie API Stripe (test mode) : `CreateSepaSetup` réel (Customer + SetupIntent),
mandat complété avec l'IBAN de test FR officiel de succès
(`FR1420041010050500013M02606`, docs.stripe.com/testing#sepa-direct-debit),
puis le SetupIntent re-récupéré SANS `expand` (exactement la forme que porte
`data.object` d'un webhook réel) injecté tel quel dans le code déjà écrit.
Fait une première fois en isolation (Stripe seul), puis une seconde fois de
bout en bout contre un vrai marchand de staging (créé puis nettoyé) en
passant par le vrai `billing.Service`/`Repository`/routes — pas seulement
contre l'API Stripe seule.

**Bug trouvé : `sepa_mandates.last4_iban_masked` restait toujours NULL.**
`HandleSetupIntentSucceeded` lisait `intent.PaymentMethod.SEPADebit.Last4`
en supposant le payload du webhook expansé — hypothèse fausse. Un
`setup_intent.succeeded` réel porte `payment_method` comme une simple
référence par id (`SEPADebit` reste `nil`), jamais l'objet complet. La
signature du mandat elle-même (statut, transition LIVE) n'était pas
affectée — seul le masquage d'IBAN, explicitement demandé par le schéma,
était silencieusement perdu.

**Corrigé** : `internal/infrastructure/stripe/billing.go` gagne
`GetPaymentMethod(id)` ; `HandleSetupIntentSucceeded` l'appelle en repli
quand `SEPADebit` n'est pas déjà présent dans le payload (jamais le cas
aujourd'hui, mais sans coût si Stripe changeait ce comportement). Test
d'intégration mis à jour pour reproduire la forme réelle (référence nue,
plus un objet en ligne) plutôt que la forme supposée initialement.

**Confirmé de bout en bout, contre un vrai marchand de staging (créé et
nettoyé pour ce test) et la vraie API Stripe** :
`CreateSepaSetup` → Customer + SetupIntent réels → mandat complété (IBAN de
test FR) → `HandleSetupIntentSucceeded` sur le payload non-expansé réel →
`merchant.activation_state = 'LIVE'`, `went_live_at` renseigné,
`subscriptions.status = 'active'`, `sepa_mandates.last4_iban_masked = '2606'`
(les 4 derniers chiffres réels de l'IBAN de test) — tout cela sans délai, sans
intervention manuelle, exactement la décision N4b.

**Non couvert par cette vérification** : les IBAN de test simulant un refus
de mandat ou un échec de prélèvement (`AT8619...`/`FR84...` etc.) n'ont pas
été exercés — B2b-0 visait la forme du webhook de mandat, pas encore la
cascade d'impayé (B2b-1, qui a son propre risque de forme sur
`invoice.payment_failed`, pas vérifié ici). Le webhook réel n'a pas non plus
été livré par un vrai POST HTTP Stripe (pas de tunnel configuré) — la forme
JSON a été confirmée par récupération API directe non-expansée, ce qui est
la même sérialisation que Stripe utilise pour `data.object`, mais le
chemin HTTP+chi lui-même (déjà utilisé sans souci par tous les autres
endpoints de ce dépôt) n'a pas été rejoué littéralement.

---

### LOT B B2a-1/2/3 — Table de facturation plateforme, mandat SEPA (2026-09-12)

Implémenté à la suite de B2a-0 (ci-dessous). Nouveau module
`internal/modules/billing` — délibérément séparé de `subscriptions` (qui
possède déjà `pricing.Repository` comme dépendance externe) et de
`internal/webhook/stripe` (qui reste le seul point d'entrée des webhooks
Stripe, dispatchant vers `billing.Service` pour les événements de ce
chantier).

**B2a-1 — `platform_billing_customers`** (migration 140), nom sans
ambiguïté avec `welloresto_stripe_customers` (confirmé lié au compte
connecté). Pas de FK vers `merchant(id)` malgré le schéma du brief : testé
directement contre staging, `merchant_id text REFERENCES merchant(id)`
échoue à la création (`SQLSTATE 42804`, types incompatibles — `merchant.id`
est `integer`) — même situation déjà documentée pour
`subscription_items`/`subscription_overrides`. Une `UNIQUE (merchant_id)`
porte l'invariant "un merchant_id, une ligne, un stripe_customer_id" à sa
place. Deux endpoints admin (`RequirePlatformAdmin`, déjà en place depuis
B1d) : `attach-to/{other_merchant_id}` (refuse si la cible a déjà des
factures Stripe sur son propre Customer — vérifié en interrogeant l'API
Stripe directement, `Invoices.List`, jamais via l'ex-`subscription_invoices`)
et `detach` (crée un nouveau Customer dédié).

**B2a-2 — création paresseuse.** `Service.resolveOrCreateBillingCustomer`
(privé, appelé par `CreateSepaSetup`) : réutilise la ligne existante telle
quelle si présente (y compris une ligne mutualisée — jamais de second
Customer créé pour un merchant déjà rattaché à un autre), sinon crée un
Customer Stripe et la ligne (`is_primary_for_merchant=true`). Contact
"propriétaire" résolu via `users_rights.admin=TRUE` le plus ancien (même
proxy déjà documenté pour la remise multi-marchand, `hasMultiMerchantOwner`),
repli sur `merchant.email`/`merchant.fullname` si aucun admin.

**B2a-3 — mandat SEPA.**
`POST /v1/billing/sepa/setup` (client, `settings.manage`) résout/crée le
Customer puis crée un `SetupIntent` `sepa_debit`/`usage=off_session` et
retourne son `client_secret`. Webhook `setup_intent.succeeded`, ajouté au
dispatcher existant (`internal/webhook/stripe/service.go`, qui appelle
désormais `billing.Service.HandleSetupIntentSucceeded`) : écrit
`sepa_mandates`, `subscriptions.status='active'`,
`merchant.activation_state='LIVE'`/`went_live_at=now()` — cette dernière
écriture gardée par `WHERE went_live_at IS NULL` (un webhook rejoué ne doit
pas déplacer la date de mise en service réelle). **Décision N4b
re-vérifiée, pas supposée** : aucune condition sur `subscription_items`, un
produit vendable, ou quoi que ce soit d'autre — testé explicitement avec un
marchand n'ayant AUCUNE ligne `subscription_items` au moment du mandat, qui
passe quand même en LIVE.

**Note technique** : `HandleSetupIntentSucceeded` prend le JSON brut de
l'événement plutôt qu'un `*stripe.SetupIntent` typé, parce que
`internal/webhook/stripe` (le dispatcher existant) est resté sur
`stripe-go v78` tandis que ce nouveau module (comme
`internal/infrastructure/stripe`) est sur `v84` — les deux coexistent dans
`go.mod` sans conflit, mais un objet typé de l'un n'est pas celui de
l'autre ; le format JSON Stripe, lui, ne change pas selon la version du SDK.
Pas de migration du dispatcher vers v84 dans ce chantier (hors scope,
risque de régression sur les webhooks Connect existants).

**Choix Elements intégré vs page hébergée — proposition, pas un choix
déjà tranché en votre nom.** Le coût CÔTÉ BACKEND des deux options est
quasi identique (un seul appel Stripe qui change : `SetupIntent.New`
retournant un `client_secret`, contre `checkout/session.New` en
`mode=setup` retournant une URL) — la vraie différence de charge de travail
est côté FRONT (`wello-back-office`, un dépôt distinct, hors périmètre de
cette session). **Implémenté ici : l'option A** (le backend retourne un
`client_secret`, prêt pour un composant Stripe Elements) — recommandé
puisque c'est la cible produit et que ça ne coûte rien de plus à construire
que B côté API. Reste explicitement à faire, ailleurs : le composant
Stripe Elements réel dans `wello-back-office`. Si cette intégration front ne
peut pas être livrée à temps, le repli B ne demande qu'un changement
contenu (`CreateSepaSetupIntent` → une `checkout.Session` en mode setup,
retourner `.URL` au lieu de `.ClientSecret`) — pas une refonte.

**Tests** : 5 tests d'intégration Postgres
(`internal/modules/billing/billing_postgres_integration_test.go`), tous
verts contre staging — première souscription (Customer créé), deuxième
établissement du même opérateur (Customer distinct, vérifié qu'un rappel
n'en recrée pas un troisième), mutualisation (+ les deux refus : source sans
Customer, cible ayant déjà des factures), détachement, passage en LIVE
(mandat seul, y compris le cas sans aucune ligne `subscription_items`, et
non-régression de `went_live_at` sur rejeu). Stripe lui-même est un faux
(`fakeStripe`, implémentant la même interface que `StripeManager`) : cet
environnement n'a pas de `STRIPE_API_KEY` (`.env` absent du dépôt, voir
CLAUDE.md), donc aucun test ici n'a pu appeler la vraie API Stripe — signalé
plutôt que contourné silencieusement. `internal/webhook/stripe`'s propre
test d'intégration a été adapté (son bloc "Subscription" testait
`CreateInvoice`/`PayInvoice` contre `subscription_invoices`, remplacé par
`SetSubscriptionStatus`/`UpdateSubscriptionBillingPeriod` contre
`subscriptions`, cohérent avec B2a-0).

Migrations 140/141 appliquées à staging (additives).

---

### LOT B B2a-0 — Vérification lecture de subscription_invoices (2026-09-12)

Réponse, avant tout code sur B2a-1/2/3 : **`subscription_invoices` n'est lue
nulle part par l'application.** Recherche exhaustive (`grep` sur tout le
dépôt Go) : les trois seules occurrences du nom de table sont
`internal/webhook/stripe/repository.go` (l'`INSERT` de `CreateInvoice` et
l'`UPDATE` de `PayInvoice`, écriture pure) et
`internal/webhook/stripe/postgres_integration_test.go` (deux `SELECT`, mais
qui vérifient l'effet de ces mêmes écritures dans le test du module — pas un
consommateur applicatif). Une seule implémentation du `Repository`
(`mysqlRepo`, malgré son nom — même motif que le reste du dépôt : les
requêtes passent par `dbx`/`Rebind` pour rester portables MySQL/Postgres),
donc pas de second chemin de lecture caché derrière un autre dialecte.
Aucun handler HTTP, export, rapport ou module analytics ne la sélectionne.

**Décision (conforme à la règle donnée) : `HandleInvoiceCreated`/`HandleInvoicePaid`
seront REMPLACÉS, pas dupliqués** — ils écriront désormais
`subscriptions.status`/`current_period_end` au lieu de
`subscription_invoices`/`repo.CreateInvoice`/`repo.PayInvoice`. La table
`subscription_invoices` elle-même n'est pas supprimée dans ce chantier (pas
demandé), seulement plus alimentée par ces deux handlers.

---

### LOT B B2a — Investigation préalable, arrêtée avant tout code (2026-09-12)

Avant d'écrire le moindre code B2a (mandat SEPA), recherche de l'existant
autour de la facturation "WelloResto facture le marchand" — un
`SetupIntent` SEPA a besoin d'un objet Stripe `Customer` auquel s'attacher,
et rien dans ce dépôt ne semblait en créer un. Deux découvertes qui changent
la donne, remontées à l'utilisateur avant toute écriture (aucun code B2a
n'a été commencé suite à cette investigation — en attente de ses indications) :

**1. `welloresto_stripe_customers` existe déjà, avec des données réelles.**
Table héritée de l'ère MySQL (`merchant_id` PK, `creator_user_id`,
`stripe_customer_id`), 5 lignes en staging, identiques aux 5 lignes déjà
présentes dans le dump MySQL d'origine (`data-migration/migration_welloresto_data.sql`) :
merchants historiques 173/196/203/212/217 (numérotation MySQL), avec des
`cus_...` qui ressemblent à de vrais identifiants Stripe Customer. Aucun
code Go n'écrit dans cette table aujourd'hui (recherche exhaustive) — elle
n'est que lue, une seule fois, dans une sous-requête de
`internal/webhook/stripe/repository.go` (`CreateInvoice`, voir point 2).
**Précision de l'utilisateur, à respecter** : cette table sert au compte
Stripe **connecté** (`stripe_accounts`), pas à la relation
"marchand-client-payeur-de-WelloResto" que B2a doit construire — donc PAS le
bon endroit pour stocker le `stripe_customer_id` du mandat SEPA. Observation
factuelle à noter en tension apparente avec cette lecture, sans trancher :
`merchant_id=212` (Croq'Ô'Pizzas, déjà croisé au P2 du chantier précédent)
a une ligne ici avec `creator_user_id` correspondant à un des deux admins
réels de ce marchand (`user_id=226`, `croqopizzas4@gmail.com`) — cohérent
avec un Customer représentant le marchand lui-même plutôt qu'un artefact du
compte connecté. Remonté tel quel, pas interprété plus loin.

**2. `invoice.created`/`invoice.paid` sont déjà des webhooks Stripe gérés en
production** (`internal/webhook/stripe/service.go`, section "7. Invoices
(Subscription)") — exactement les deux noms d'événements que B2b demande
d'ajouter. Le comportement actuel : `HandleInvoiceCreated` lit
`invoice.Metadata["merchant_id"]` et appelle `repo.CreateInvoice`, qui
insère dans `subscription_invoices` (`status`/`amount`/`payment_date`) SOUS
RÉSERVE qu'une ligne `welloresto_stripe_customers` existe pour le
`stripe_customer_id` de la facture (jointure de garde, pas de lecture réelle
de colonne) ; `HandleInvoicePaid` appelle `repo.PayInvoice`. Aucun des deux
ne touche `subscriptions.status`/`current_period_end` — la table
`subscription_invoices` est distincte du modèle LOT B B1b/B1c. Usage
recherché exhaustivement dans tout le dépôt (code Go, docs) : ni l'un ni
l'autre n'est lu ailleurs que dans ce même fichier
(`internal/webhook/stripe/repository.go` et son test d'intégration) — aucun
handler HTTP, export comptable ou module de reporting ne les expose. En
staging : `subscription_invoices` a 0 ligne, `welloresto_stripe_customers`
en a 5 (voir point 1) — le chemin d'écriture existe et est appelé (le
handler est bien branché dans `ProcessEvent`), mais rien ne prouve depuis le
code seul si le webhook Stripe réel envoie encore ces événements
aujourd'hui pour ces 5 marchands historiques.

**En attente des indications de l'utilisateur avant de reprendre B2a** —
notamment : où stocker le `stripe_customer_id` du mandat SEPA si ce n'est
pas `welloresto_stripe_customers`, et si `HandleInvoiceCreated`/`HandleInvoicePaid`
doivent être étendus (garder l'écriture `subscription_invoices` existante et
ajouter les écritures `subscriptions.status`/`current_period_end`) ou
remplacés.

---

### LOT B B1d/B1e — Suite : préalable, dérogations, aperçu (2026-09-12)

Jour 1 du brief "LOT B — Suite" : PRÉALABLE (P1/P2/P3) + B1d (dérogations
commerciales) + B1e (aperçu de changement). Arrêté ici pour rapport avant
B2a/B2b/B2c, comme demandé. Rien commité (attente d'une demande explicite).

**P1 — corrigé.** `ErrSubscriptionItemPriceUnavailable` répondait 501
(`internal/models/responses_models.go`). Remplacé par 409, code
`pricing_unavailable_for_code` — un état métier attendu (kiosk/sms sans prix
grille), pas un endpoint manquant. Le sentinel Go (`ErrSubscriptionItemPriceUnavailable`)
n'a pas été renommé, seul le mapping HTTP a changé.

**P2 — investigué, non tranché (comme demandé).** Le seul admin staging avec
4 marchands est `user_id=2` (`iliesbellal@gmail.com` — le compte de
l'opérateur de cette session). Détail par marchand :

| merchant_id | nom | SIRET | tél | créé le |
|---|---|---|---|---|
| 2 | Brasserie du midi | 65948751326549 | +33609217928 | 2022-04-20 |
| 303 | BdM 2 | 12345678900 (factice — 11 chiffres, pas un SIRET valide) | +33609217928 | 2026-08-29 |
| 212 | Croq'Ô'Pizzas | 419750591 | +33387513569 | 2024-05-19 |
| 230 | Ok Pizza | 81860975000014 | +33387661154 | 2025-09-03 |

Merchants 2 et 303 partagent SIRET-motif/téléphone/email avec le compte
lui-même — 303 ("BdM 2") ressemble à un clone de test du merchant 2, créé il
y a deux semaines, pas un second établissement réel. Merchants 212 et 230 ont
des SIRET et téléphones distincts et **chacun un autre admin réel déjà
enregistré** (212 : `croqopizzas4@gmail.com`, `slimani_nabil@hotmail.com` ;
230 : aucun autre admin trouvé) — ce ne sont pas des établissements de
`user_id=2`.

Conclusion factuelle (confiance haute pour 212, moyenne pour 230, faute
d'un second signal indépendant sur celui-ci) : ce n'est PAS le cas (a) — un
propriétaire réel de 4 établissements. C'est plus proche du cas (b), avec
une nuance : `user_id=2` semble être un compte interne (l'opérateur de ce
dépôt) avec accès admin sur des marchands clients pour support/test, plus un
marchand personnel réel (2) et son clone de test (303). La règle de remise
multi-marchand (B1c, déjà livrée) s'applique donc aujourd'hui à un compte qui
n'est très probablement pas un franchisé — à corriger dans un chantier
séparé une fois confirmé (pas dans celui-ci, conformément à la consigne
"ne pas trancher, remonter").

**P3 — implémenté.** Avant ce chantier, `Repository.AddItem` n'interrogeait
pas `pricing_catalog` du tout (le prix était un paramètre fourni par
l'appelant) — kiosk/sms auraient pu être écrits sans aucun garde-fou. Ajouté :
- `subscriptions.Service.AddItem` (nouveau point d'entrée gardé — les
  handlers l'utilisent, jamais `Repository.AddItem` directement) vérifie
  `pricing_catalog` avant écriture.
- `subscriptions.resolveUnitPriceCents`/`PriceAvailableForCode` : source
  unique de "ce code a-t-il un prix", partagée par `ComputeSubscriptionAmount`,
  `AddItem`, `ApplyItemChanges` (B1e) et la garde de dérogation (B1d) — un
  code devient inscriptible exactement quand il devient calculable, jamais
  avant, sans risque de divergence entre deux implémentations dupliquées.
- Côté B1d, `CreateOverride` rejette toute dérogation `kind=module` visant
  `kiosk`/`sms` avec le même `ErrSubscriptionItemPriceUnavailable` (409).
  Interprétation retenue : la garde P3 porte sur les dérogations qui
  contournent la facturation d'un *code subscription_items* (module/price) —
  pas sur `kiosk_quota`, un mécanisme opérationnel préexistant
  (`subscriptions.max_kiosks`, déjà utilisé par `kiosk.Repository.GetMerchantMaxKiosks`
  avant ce chantier) sans rapport avec `pricing_catalog`. À confirmer que
  cette lecture est la bonne — c'est une interprétation d'un point ambigu du
  brief, pas une évidence.

**B1d — implémenté.** Table `subscription_overrides`
(`migrations/todo/139_subscription_overrides.up.sql`) + module
`internal/modules/subscriptions` (overrides_repository.go/overrides_service.go)
+ 3 endpoints (`POST /v1/admin/merchants/{id}/overrides`,
`GET /v1/admin/overrides`, `DELETE /v1/admin/overrides/{id}`), appliquée à
staging (voir plus bas).

*Permission interne — décision prise avec l'utilisateur, pas silencieuse.*
Aucune notion de "staff WelloResto" cross-tenant n'existait dans ce dépôt
(le seul précédent, `/admin/upsell`, porte un TODO explicite l'attendant).
Après consultation : colonne `users.is_platform_staff` (migration 138,
`ADD COLUMN ... DEFAULT false` — personne n'est basculé automatiquement) +
middleware `middleware.RequirePlatformAdmin`, indépendant de
`RequirePermission`/`settings.manage`. **Suite nécessaire avant tout test
end-to-end en staging** : aucun compte n'a `is_platform_staff = true`
aujourd'hui — un `UPDATE` manuel ciblé sera nécessaire (pas fait ici, hors
scope de ce chantier de code).

*`target` — schéma sans colonne "valeur" dédiée, donc une convention a dû
être choisie :*
- `kind='module'` : `target` = un code façon `subscription_items`
  (`reservation`, `haccp`, `planning`, `delivery`, `kiosk` — mappés
  respectivement vers `bookings_enabled` [note : `reservation` est nommé
  `bookings_enabled` en base, écart déjà connu ailleurs dans ce document],
  `haccp_enabled`, `planning_enabled`, `delivery_enabled`, `kiosks_enabled`).
  `marketplaces` n'a **aucune** colonne `*_enabled` en base — rejeté
  explicitement (`ErrOverrideTargetUnsupported`), pas silencieusement ignoré.
  `kiosk`/`sms` en plus bloqués par P3.
- `kind='price'` : `target` = la valeur entière de `override_price_cents`
  (en texte).
- `kind='kiosk_quota'` : `target` = la nouvelle valeur de
  `subscriptions.max_kiosks` (en texte).

*Révocation — ne défait pas l'effet.* `RevokeOverride` positionne
`revoked_at` mais ne réinitialise pas la colonne `*_enabled`/
`override_price_cents`/`max_kiosks` que la dérogation avait modifiée : aucune
valeur "avant" n'est tracée nulle part pour y revenir, et en deviner une
(ex. remettre `FALSE`) serait faux si le module était déjà activé
indépendamment de la dérogation. Remise en état manuelle par le staff en
parallèle de la révocation, pour l'instant — **question ouverte**, à trancher
avant B2 si des dérogations réelles doivent être créées/révoquées en
production.

**B1e — implémenté.** `GET /v1/subscriptions/preview?add=...&remove=...` et
`POST /v1/subscriptions/items`, gardés par `settings.manage` (client-facing).
Aucune écriture côté preview — vérifié par test (`ListActive` inchangé après
appel).

*Prorata — approximation assumée.* `subscriptions.current_period_end` reste
`NULL` tant qu'un cycle de facturation réel n'a pas démarré (LOT B2, mandat
SEPA) — la quasi-totalité du staging aujourd'hui. Repli : mois nominal de 30
jours (delta appliqué en entier). À revoir une fois B2 câblé sur de vraies
périodes Stripe — **signalé, pas silencieux**.

*Comparaison pack vs à la carte (§7.7) — normalisée en mensuel des deux
côtés.* `pricing.Service.ResolveCheapestPlan` utilise
`pricing_catalog.annual_price_cents` (un taux annuel pré-calculé, chantier
11) tandis que ce module applique sa propre règle annuelle (B1c, règle 4 :
×10 sur le total mensuel) — deux conventions annuelles non interchangeables,
déjà documentées comme telles dans `amount.go` avant ce chantier. Combiner
les deux directement aurait comparé des choses non comparables ; la
comparaison §7.7 tourne donc toujours en mensuel (`Cart.BillingCycle="monthly"`
et le total à la carte ramené au mensuel via division par 10 si le cycle est
annuel), tandis que `current_total_cents`/`new_total_cents` restent sur le
vrai cycle du marchand. **Simplification assumée, à revalider** — réconcilier
proprement les deux conventions annuelles serait un chantier à part entière,
pas quelque chose à improviser ici.

**Tests.** 21 tests d'intégration Postgres (`-tags postgres_integration`,
`internal/modules/subscriptions/*_postgres_integration_test.go`), tous verts
contre staging : création de dérogation (les 3 `kind`), révocation (+ double
révocation, + id inconnu), rejet P3 (kiosk/sms), rejet cible non mappée
(marketplaces), kind/reason invalides, aperçu avec et sans bascule de pack
(calculé à partir des vrais prix `pricing_catalog` de staging : essentiel
79,00 / pro 129,00 / complet 189,00 / réservation 59,00 — ajouter réservation
seule à un essentiel fait bien basculer vers pro à 129,00 < 138,00), rejet
d'ajout non facturable en preview et en apply, application réelle
add+remove avec `override_price_cents` intact. Trois tests `auth` cassés par
l'ajout de la colonne `is_platform_staff` au SELECT partagé
(`GetUserByToken`/`Login`/`GetUserByPIN`) ont été corrigés (décalage d'index
dans les mocks `sqlmock`) — pas de régression restante. `go test ./...`
montre par ailleurs des échecs préexistants et sans rapport
(`planning/employees`, `planning/leave`, `planning/swaps`, `ubereats`) —
aucun fichier de ces paquets n'a été touché par ce chantier, vérifié via
`git status`.

**Migrations appliquées à staging** (comme la 137 l'était déjà avant ce
chantier) : 138 (`users.is_platform_staff`) et 139 (`subscription_overrides`),
toutes deux strictement additives (`ADD COLUMN IF NOT EXISTS`,
`CREATE TABLE`).

**Questions ouvertes pour la suite (pas tranchées silencieusement) :**
1. P2 : qui décide si la règle de remise multi-marchand doit exclure les
   comptes staff internes ?
2. La révocation d'une dérogation doit-elle réellement défaire l'effet
   (remettre le module à faux, effacer l'override de prix, restaurer le
   quota kiosk précédent) ? Si oui, il faut décider quoi stocker comme
   "valeur avant" à la création.
3. `marketplaces` comme cible de dérogation `module` : faut-il lui donner une
   vraie colonne `*_enabled`, ou est-ce hors de portée de `subscription_overrides` ?
4. Qui bascule le·s premier·s compte·s `is_platform_staff = true` en
   staging, pour permettre un test end-to-end des endpoints B1d ?

---

### LOT B B1 — Préalable (2026-09-12)

Trois correctifs de cinq minutes demandés avant le chantier B1 (modèle
d'abonnement), traités avant toute écriture de code sur B1a-e.

**1. Validation au démarrage — appliquée.** `GOOGLE_CLIENT_ID` et
`SIGNUP_CONTEXT_SIGNING_KEY` sont désormais `log.Fatal` au démarrage si
absentes, sur le modèle exact de `PIN_PEPPER`/`FISCAL_SIGNING_KEY`
(`internal/config/config.go`). Les deux avaient été rendues délibérément
non bloquantes au LOT A (voir l'entrée du 2026-09-11 ci-dessous) — décision
maintenant inversée par ce brief. **Point d'attention avant déploiement** :
`docs/decisions.md` ne dit nulle part si ces deux variables sont
effectivement positionnées sur les environnements Render (staging et
production) — le dépôt ne contient aucun `render.yaml` ni équivalent pour
le vérifier depuis le code. Si l'une des deux est absente d'un
environnement déployé, ce correctif transforme un comportement dégradé
(clé aléatoire en mémoire / erreur runtime sur `/v1/auth/google`) en
crash-loop au démarrage. À confirmer côté Render avant de merger/déployer.
`internal/modules/onboarding` et les tests d'intégration Postgres ne
passent pas par `config.Load()` (ils utilisent `pgtest.Open` directement) :
aucun test existant cassé par ce changement — vérifié par `go build ./...`.

**2. Vérification Google id_token à `GOOGLE_CLIENT_ID` vide — déjà correcte,
aucune faille trouvée.** `googleauth.Verifier.Verify`
(`internal/modules/googleauth/verifier.go:87`) retourne une erreur
immédiatement si `v.clientID == ""`, avant même de tenter de parser le
jeton — aucune branche du code n'accepte un jeton quand `GOOGLE_CLIENT_ID`
est vide. Ce n'est pas un contournement d'authentification : c'est un échec
fermé. Rien à corriger ; consigné ici pour clore explicitement le point du
brief.

**3. Écart de nommage `onboarding_tasks` — décrit, non appliqué (170 lignes
existantes en staging, présumées de taille comparable en production).**

Colonne réellement en base (migration `129_onboarding_tasks`, livrée LOT A
Semaine 2 chantier 6c) : `task_key TEXT NOT NULL`, exposée telle quelle dans
l'API (`Task.TaskKey` avec `json:"task_key"` — `internal/modules/onboarding/models.go`).
Colonne attendue par `docs/WelloResto-Parcours-Client-v2.docx` §8.4 :
`code TEXT NOT NULL`.

Ce n'est pas qu'un renommage de colonne, pour trois raisons :
- **`task_key` est un champ de réponse JSON public**, consommé par au moins
  `wello-back-office` (écran de liste de démarrage) et potentiellement
  `wello_resto_flutter`/`wello-kiosk` s'ils lisent cet endpoint. Renommer la
  colonne SQL sans renommer le champ JSON ne change rien pour les clients ;
  renommer les deux casse le contrat d'API en place tant que les fronts ne
  sont pas mis à jour en même temps — coordination inter-dépôts, pas une
  migration isolée.
- **Un deuxième écart de nommage existe sur la même table** :
  `skip_reason` (migration `135_onboarding_tasks_skip`, réel) vs
  `skipped_reason` (document, §8.4). Même problème d'exposition JSON
  (`json:"skip_reason,omitempty"`).
- **Le schéma du document a trois colonnes qui n'existent pas du tout** en
  base réelle : `position SMALLINT` (l'ordre réel vient de `created_at`,
  implicite, pas d'une colonne dédiée), `completed_by TEXT` (aucune
  attribution de qui/quoi a complété une tâche n'est tracée aujourd'hui), et
  `metadata JSONB DEFAULT '{}'` (aucune extensibilité par ligne). Un
  `ALTER TABLE ... RENAME COLUMN` ne crée pas ces colonnes ; il faudrait un
  vrai chantier de mise à niveau du modèle, pas un correctif de nommage.
  Le document liste aussi un statut `in_progress` inexistant côté code
  (`pending | done | skipped` réels vs `todo | in_progress | done | skipped`
  documentés) — encore un écart, pas traité ici.

**Résolution proposée (non appliquée)** : traiter ceci comme son propre
petit chantier plus tard (hors B1, puisque B1 ne touche pas `onboarding_tasks`) —
migration additive (`ALTER TABLE ... RENAME COLUMN task_key TO code`,
`RENAME COLUMN skip_reason TO skipped_reason`, + `ADD COLUMN position`,
`completed_by`, `metadata` avec des valeurs par défaut rétro-compatibles),
suivie d'une mise à jour coordonnée du struct Go, du handler, et des trois
front-ends consommateurs dans la même fenêtre de déploiement. Ne pas
renommer la colonne SQL sans renommer le champ JSON en même temps (contrat
à moitié migré = pire que l'écart actuel).

---

### LOT B B1a — Validation du plan (2026-09-12)

**Validation applicative ajoutée.** `pos.POSRepository.InsertSubscription`
(`internal/modules/pos/create_repository.go`) vérifie désormais que
`packageID` existe dans `packages` avant l'`INSERT`, et retourne
`models.ErrUnknownPackageID` (nouveau sentinel, `internal/models/responses_models.go`,
mappé 400 `unknown_package_id` dans `SendErrorJSON`) sinon. Toujours pas de
contrainte de clé étrangère sur `subscriptions.package_id` (elle échouerait
tant que la ligne orpheline existe — voir ci-dessous — et le calendrier ne
le permet pas) : cette vérification côté code est la seule garde pour
l'instant, notée comme suite à faire une fois l'orpheline traitée. Testé
contre le Postgres de staging (`TestInsertSubscription_UnknownPackageID_Postgres`,
`internal/modules/pos/postgres_integration_test.go`) : rejet du
`package_id=-4` sans écrire de ligne, insertion normale toujours acceptée.

**Ligne orpheline — investiguée en détail, résolue par explication plutôt
que par correction de données.** `subscriptions.id=91` en staging :
`merchant_id='-217'`, `package_id=-4` (aucune correspondance dans
`packages`). Recherche complète avant de trancher :
- `merchant.id=217` ("OK PIZZA", `is_active=false`) existe bien, mais rien
  ne le lie formellement à `-217` — le signe négatif n'est pas un pointeur
  vers `217` en clair.
- Une deuxième ligne du même type existe : `subscriptions.id=101`,
  `merchant_id='-230'`, mais avec un `package_id=3` **valide** — donc pas
  détectée par le contrôle FK-like ajouté ci-dessus, qui ne porte que sur
  `package_id`.
- Trois marchands quasi-homonymes coexistent en base ("OK PIZZA" 217 inactif,
  "Ok Pizza" 230 actif sans abonnement valide, "OK Pizza" 237 actif avec un
  abonnement sain `id=108, package_id=3`) — signe d'un historique de
  recréation de compte pour le même restaurant, qui a d'abord fait
  soupçonner une corruption de données (signe négatif = ancien pointeur vers
  le marchand courant, à corriger).

**Confirmé par l'utilisateur** : le signe négatif sur `merchant_id` (et donc
sur `package_id`, négé en même temps) est une ancienne convention manuelle
pour désactiver temporairement un abonnement — pas une corruption, pas un
pointeur cassé à réparer. Ces deux lignes (`91`, `101`) sont des lignes
mortes : aucun code applicatif actuel ne les lit, ne les écrit, ni ne
dépend du signe négatif (recherché explicitement — aucune fonction
`Disable`/`Pause` de ce type dans le code actuel). **Aucune donnée
modifiée** : pas de réattribution de plan, la ligne reste en l'état. La
seule action utile était la garde applicative ci-dessus, pour qu'un
**nouvel** abonnement ne puisse plus se retrouver dans le même état.

---

### LOT B B1b — Lignes de facturation (2026-09-12)

**Migration 137, appliquée sur le Postgres de staging** (vérifié :
`subscription_items` créée, colonnes `subscriptions` ajoutées, 64/64
abonnements existants backfillés à `active` — pas un seul resté à `setup`).
`migrations/todo/137_subscription_items.{up,down}.sql`.

- `subscription_items` — ce qui est FACTURÉ, distinct de
  `subscriptions.*_enabled` (ce à quoi le marchand a ACCÈS, inchangé) :
  `id, merchant_id, code, kind, quantity, unit_price_cents, created_at,
  updated_at, removed_at`. Pas de CHECK constraint sur `kind`/`code` (même
  convention que `pricing_catalog.kind`) — validé côté applicatif
  (`subscriptions.ValidKinds`/`ValidCodes`, closed set exact du brief).
  Index unique partiel `(merchant_id, code) WHERE removed_at IS NULL` :
  une seule ligne active par code et par marchand, pour empêcher une double
  facturation — pas demandé explicitement par le brief mais découle
  directement du modèle qu'il décrit (une ligne "retirée" doit pouvoir être
  remplacée, pas coexister avec son remplacement).
- **Écart de préfixe d'id, assumé** : le brief demande `sbit_`
  (underscore), mais `helpers.GeneratePrefixedID` — utilisé par toutes les
  tables de ce type dans ce dépôt (`onb-`, `prst-`, `ctx-`) — génère
  systématiquement `prefix-<uuid>` (tiret). Choix : aligné sur la
  convention réelle du code plutôt que sur la notation du brief, comme déjà
  observé sur `onboarding_tasks` (`onbt_` documenté vs `onb-` réel, voir
  l'entrée du préalable ci-dessus). Un id est une clé opaque, jamais un
  contrat exposé — contrairement au nom d'une colonne JSON, ce n'est pas le
  genre d'écart qui casse un client.
- `subscriptions` : quatre colonnes ajoutées — `override_price_cents`
  (nullable, NULL = tarif de grille), `billing_cycle` (`monthly` par
  défaut), `current_period_end` (nullable), `status` (`setup` par défaut,
  **backfillé à `active`** pour tout marchand existant, même vigilance que
  `activation_state` en semaine 2/migration 125 — sans le backfill, les 64
  abonnements de staging seraient tous repassés "en cours de
  configuration").
- Nouveau module `internal/modules/subscriptions` (`models.go`,
  `repository.go`) : `Item`, constantes `Kind*`/`Code*`, `Repository.AddItem`
  (valide kind/code, retourne `models.ErrInvalidSubscriptionItemKind`/
  `ErrInvalidSubscriptionItemCode` sinon — sentinelles centralisées dans
  `internal/models/responses_models.go`, même convention que
  `ErrUnknownPackageID`), `RemoveItem` (soft, `removed_at`), `ListActive`
  (l'entrée du calcul du chantier B1c). Pas de handler HTTP dans ce
  chantier — B1b ne demande que la table et son modèle, pas d'endpoint.
  Testé contre le Postgres de staging
  (`TestRepository_Postgres`, `internal/modules/subscriptions/repository_postgres_integration_test.go`) :
  kind/code invalides rejetés, doublon actif rejeté par l'index, retrait
  puis réajout du même code accepté.

**Non traité ici, à noter pour B1c** : le brief dit que le calcul du
montant (hors dérogation) doit utiliser "les prix de la table de référence
tarifaire" (`pricing_catalog`, chantier 11) plutôt que
`subscription_items.unit_price_cents` — cette dernière colonne n'est donc
qu'un instantané pour l'historique de facturation, jamais relue pour le
calcul en cours. Documenté dans le commentaire de la migration pour ne pas
l'oublier au chantier suivant.

---

### LOT B B1c — Calcul du montant (2026-09-12)

`subscriptions.Service.ComputeSubscriptionAmount` (`internal/modules/subscriptions/amount.go`),
les cinq règles du brief, dans l'ordre. **Le service de tarification du
chantier 11 (`pricing.Repository.LoadCatalog`) est bien réutilisé** — pas de
grille dupliquée — mais sa structure ne couvre pas tout le périmètre de
B1c, pour deux raisons concrètes trouvées en la parcourant, pas supposées :

- **`kiosk`** (code `subscription_items` valide, accepté par `AddItem`) n'a
  pas de prix unique dans `pricing_catalog` : la borne y est modélisée en
  trois lignes `addon` distinctes (`kiosk_first_tier1`,
  `kiosk_additional_tier1`, `kiosk_tier2` — palier temporel + 1ère/
  supplémentaire, chantier 11a). Choisir l'une des trois arbitrairement
  aurait été une facturation incorrecte silencieuse.
- **`sms`** (code `subscription_items` valide lui aussi) n'a **aucune**
  ligne dans `pricing_catalog`, sous aucun `kind`.

Dans les deux cas, `ComputeSubscriptionAmount` retourne
`models.ErrSubscriptionItemPriceUnavailable` (nouveau sentinel, mappé 501
`subscription_item_price_unavailable`) plutôt que de facturer 0 ou un tarif
inventé — testé (`TestComputeSubscriptionAmount_UnsupportedCode_Postgres`).
Pas bloquant pour B1c (rien ne crée encore de ligne `kiosk`/`sms` en usage
réel), mais à lever avant qu'un vrai marchand ait l'une de ces deux lignes
actives : soit ajouter les prix manquants à `pricing_catalog`, soit décider
que `kiosk`/`sms` ne passent jamais par ce calcul générique (pré-requis =
une décision produit, pas un choix technique — non tranché ici).

**Écart de nom, ponté sans le corriger** : `subscription_items` utilise
`extra_pos`, `pricing_catalog` utilise `extra_seat` pour le même concept
(poste de caisse supplémentaire) — un simple alias câblé dans
`amount.go` (`extraPOSCatalogCode`), pas une donnée à renommer dans l'une
des deux tables.

**Règle 3 (quantités variables)** : `planning_employee` et `extra_pos` sont
toujours recalculés à l'appel (`employees`/`cash_desks`, comptage direct —
même posture que les lectures croisées d'`onboarding.Repository`), jamais
lus depuis `subscription_items.quantity` — testé explicitement avec une
quantité stockée volontairement fausse (99) pour prouver qu'elle est
ignorée. "Le plan" pour le seuil des 10 salariés gratuits est déterminé
depuis la ligne `subscription_items` active de `kind='plan'` (donc ce qui
est **facturé**), pas depuis `subscriptions.package_id` (ce à quoi le
marchand a **accès**) — cohérent avec tout le principe du §7.1 : c'est bien
le plan payé qui doit déterminer la franchise, pas le plan auquel le
marchand a accès par ailleurs (dérogation).

**Règle 4 (cycle annuel)** : multiplicateur ×10 appliqué au total mensuel
de la grille, uniforme sur toutes les lignes. Délibérément **distinct** de
`pricing_catalog.annual_price_cents` (déjà une remise figée pour les trois
plans, utilisée uniquement par `pricing.Service.ResolveCheapestPlan` pour
le devis d'un nouveau prospect) — les deux mécanismes ne sont jamais
combinés ici : ce calcul relit toujours `monthly_price_cents`, jamais
`annual_price_cents`.

**Règle 5 (remise multi-établissement)** : ce schéma n'a pas de colonne
"propriétaire" dédiée — seul `users_rights.admin` (booléen déjà existant,
posé à la création du marchand) s'en approche. Interprétation retenue,
assumée et non tranchée avec l'utilisateur : la remise s'applique dès qu'un
admin du marchand administre aussi un autre marchand — vérifié en staging
que ce cas existe réellement (plusieurs utilisateurs administrent 2 à 4
marchands). À confirmer si "propriétaire" doit un jour désigner une
personne unique plutôt que n'importe quel admin.

**Ordre d'application retenu** (le brief ne le précise pas explicitement) :
override (si présent) court-circuite tout le reste ; sinon, prix de grille
→ cycle annuel (×10) → remise multi-établissement (10 %, sur le total déjà
annualisé). Le détail (`Breakdown`) retourné reste toujours celui de la
grille, y compris quand `override_price_cents` s'applique — conforme à "le
détail est quand même calculé et retourné, pour affichage comparatif".

Testé contre le Postgres de staging
(`internal/modules/subscriptions/amount_postgres_integration_test.go`) :
calcul nominal, remplacement par tarif dérogatoire, quantité `planning_employee`
variable (13 salariés, plan Pro → 3 facturés), `extra_pos` variable (3 caisses
→ 2 facturées), cycle annuel (×10), remise multi-établissement (10 %, vérifié
avec deux marchands réels administrés par le même utilisateur), code sans
prix (`kiosk`) rejeté, abonnement introuvable rejeté.

---

### LOT A Semaine 3 — Découverte du document de référence et correctifs (2026-09-11)

`docs/WelloResto-Parcours-Client-v2.docx` est apparu dans le dépôt en cours de
session (déposé par l'utilisateur, sans préavis explicite) — c'est le
document de référence introuvable depuis le début de ce lot. Lu en entier
sur les sections touchant les chantiers 9 à 14 (extraction texte via
`unzip`+`sed` sur `word/document.xml`, aucun outil docx dédié disponible).
Plusieurs écarts trouvés avec ce qui avait déjà été livré et testé cette
semaine — certains corrigés immédiatement (accord explicite de
l'utilisateur sur les trois points ci-dessous), d'autres seulement
consignés faute d'un chantier dédié pour les traiter.

**Corrigés dans cette session, avec l'accord explicite de l'utilisateur** :

1. **Formule §4.4 du pack le moins cher — entièrement refaite.** La version
   livrée plus tôt reposait sur une simplification verbale ("Essentiel =
   base only") qui s'est révélée fausse : le document donne
   `coût_à_la_carte = 79 + Σ(modules) + planning(29 + 2,50×salariés) +
   25×postes` comme option à part entière dans l'argmin, pas une exclusion.
   Différence structurelle supplémentaire découverte en lisant §1.3 : le
   planning n'est jamais un des "2 modules au choix" de Pro — Pro et Complet
   incluent le planning jusqu'à 10 salariés en base, une ligne séparée des 2
   modules choisis parmi {reservation, haccp, marketplaces, delivery}.
   Réécrit dans `internal/modules/pricing/{models,service}.go` avec 7 tests
   (`TestResolveCheapestPlan_Postgres`, dont un qui prouve explicitement que
   le planning ne prend pas un des deux emplacements gratuits de Pro).
2. **`POST /v1/public/signup-context` — payload et token entièrement
   refaits.** Le document donne un panier imbriqué
   (`cart: {modules, employees, kiosks, extra_pos, billing_cycle}` +
   `attribution: {utm_source, landing, referrer}`), une réponse
   `resolved_plan: {plan_code, monthly_total_cents, breakdown}` +
   `recommended_channel`, et surtout un `context_token` **signé HS256** avec
   un **TTL de 24h** — pas un id opaque adossé à une ligne `signup_sessions`
   avec un TTL de 7 jours inventé faute de mieux. Le jeton est maintenant un
   vrai JWT sans état (`internal/modules/signup/context_token.go`,
   `golang-jwt/jwt/v5`, déjà une dépendance du dépôt) : aucune ligne
   `signup_sessions` n'est plus créée pour un contexte — `GetContext` décode
   le jeton directement, sans aller en base. Clé de signature
   (`SIGNUP_CONTEXT_SIGNING_KEY`, nouvelle variable d'environnement) :
   repli sur une clé aléatoire en mémoire si absente (les jetons ne
   survivent alors pas à un redémarrage ni ne se vérifient entre plusieurs
   instances) plutôt qu'un échec au démarrage — rien n'en dépend encore en
   production, donc pas de raison de bloquer le déploiement sur une variable
   qui n'existe pas encore. `recommended_channel` reste toujours
   `"self_serve"` faute de règle métier trouvée pour `"assisted"`.
3. **`POST /v1/signup` — restructuré selon §5.6 exactement**, avec l'accord
   explicite de l'utilisateur de casser le contrat existant plutôt que de le
   contourner. Nouveau corps `{context_token, identity: {provider, id_token
   | email+password, first_name, last_name}, merchant: {full_name, address,
   zip_code, city, country, lat, lng, tel, email, siret, place_id},
   preset_code, accepts_terms, accepts_marketing}` — remplace l'ancien corps
   plat. `merchant.address` redevient une chaîne unique (plus de
   street/street_number séparés : la sélection Google Places du tunnel ne
   les distingue pas) ; `lat`/`lng`/`place_id` sont désormais réellement
   écrits sur `merchant` (colonnes déjà existantes, jamais alimentées avant
   aujourd'hui — même famille de dette que `vat_number` au chantier 12) ;
   `accepts_terms`/`accepts_marketing` écrits sur `users.terms_of_use_accepted`
   (colonne déjà existante, jamais écrite) et `users.accepts_marketing`
   (nouvelle, migration 136). `merchant.signup_channel`/`signup_source`
   (colonnes de la migration 125, jamais alimentées non plus) reçoivent
   désormais `"self_signup"` et l'attribution décodée du jeton de contexte.
   **Message dédié pour le SIRET déjà pris** (§5.7 : « Cet établissement
   semble déjà enregistré. Contactez-nous pour être rattaché. ») — distinct
   du message générique réutilisé pour l'e-mail déjà pris, cette fois avec
   le texte exact du document plutôt que la réutilisation initialement
   demandée par le brief du chantier 10 (le document, trouvé après, est plus
   spécifique et fait autorité). `internal/modules/pos/create_models.go` et
   `create_repository.go` étendus pour porter lat/lng/place_id/signup_channel/
   signup_source sur `CreateMerchantRequest` — `/pos/create` continue de ne
   rien y écrire (ces champs restent vides pour un marchand créé par un
   membre du staff).
4. **NAF → archétype (écran 3, §5.5.1) — table réelle substituée** à la
   supposition du chantier 14 (qui plaçait à tort 56.10A/56.10C sur
   "pizzeria"). Table réelle : 56.10A→traditional, 56.10C→snack,
   56.30Z→brasserie, 10.71C/10.71D→bakery, 47.81Z→snack — "pizzeria" et
   "fast_food" ne sont jamais atteints par code NAF, seulement par choix
   manuel ou par le segment du site. Corrigé dans
   `wello-back-office/src/types/signupTunnel.ts`.
5. **Consentement (`accepts_terms`/`accepts_marketing`)** ajouté à l'écran 3
   du tunnel (deux cases à cocher, la première obligatoire pour activer
   « Créer mon compte ») — absent de la première version du tunnel puisque
   le champ n'existait pas encore côté API.

**Petit ajout backend fait au passage** (nécessaire pour l'écran de mot de
passe forcé du chantier 14, jamais construit avant faute de moyen de
détecter côté frontend qu'un compte Google n'a pas de mot de passe) :
`GET /v1/auth/password/needs-set` — sa propre requête isolée à une table
(`AuthRepository.NeedsPasswordSet`), délibérément PAS une colonne ajoutée à
la requête de login partagée (74 colonnes, `scanUserLoginRow`, utilisée par
chaque requête authentifiée) : le risque de casser ce chemin critique pour
un besoin ponctuel n'en valait pas la peine. `wello-back-office` : nouvelle
page `SetPassword.tsx`, vérifiée via `useQuery` dans `ProtectedRoute`
(mise en cache par token de session, jamais réinterrogée à chaque
navigation).

**Consigné mais volontairement non traité maintenant** (hors du périmètre
des trois points d'accord explicite ; nécessiterait un chantier dédié) :
- Le flux Google du §5.2.2 (`POST /v1/auth/google` en pré-vérification —
  rattachement automatique, refus si mot de passe existant, création
  `PENDING_ONBOARDING` sans marchand) décrit une mécanique différente de
  `googleauth`/`signupGoogle` existants (qui créent utilisateur + marchand
  en un seul appel, sans étape de pré-vérification séparée). Ces deux
  designs ne sont pas nécessairement contradictoires (le premier pourrait
  n'être qu'un contrôle amont avant le même appel unique final), mais ça
  n'a pas été vérifié ni implémenté — le tunnel ne fait aujourd'hui qu'un
  décodage client du JWT Google pour préremplir les champs, sans appel à
  `/v1/auth/google` en amont ni logique de rattachement §5.2.3.
- Réponse structurée pour `/v1/auth/google` seul (`{token, user, next_step}`)
  non alignée avec l'existant (`{status, merchant_id, user_id, token}`).
- `onboarding_tasks` (§8.4) utilise `task_key`/`pending`/`done` (chantier
  6c, avant cette session) là où le document nomme les colonnes différemment
  (`code`, statuts `todo|in_progress|done|skipped`, `position`,
  `completed_by`, `metadata`) — écart pré-existant, non introduit cette
  semaine, non corrigé (implique un changement de schéma plus large que
  celui traité ici).
- Reprise multi-appareil (§5.7 : « chaque écran persiste côté serveur à sa
  validation ») non implémentée pour les écrans 1/2 du tunnel — seul le
  `context_token` (une vraie ressource serveur) survit à un changement
  d'appareil ; le reste de l'état vit en `sessionStorage` (protège contre un
  rafraîchissement accidentel sur le même appareil, pas contre un
  changement d'appareil).
- Libellés des six archétypes (§5.5.2 : "Traditionnel", "Bar-brasserie",
  "Fast-food et burger", "Snack et emporter", "Boulangerie et salon de thé")
  repris dans les vignettes du tunnel ; le contenu détaillé de chaque
  archétype (répartition zones/tables, `covers_required`, catégories) était
  déjà celui donné directement par l'utilisateur au chantier 9 et concorde
  avec §5.5.2 — pas de changement nécessaire là.

**Exécuté** : `go build ./...` vert ; migration 136 appliquée sur
`staging` ; API lancée en local (`go run ./cmd/api`) contre `staging`,
tunnel piloté par Playwright dans un vrai navigateur (Chromium, installé
temporairement) — parcours complet re-vérifié avec les nouveaux contrats
(JWT de contexte réel décodé côté `curl`, cases à cocher testées : le
bouton "Créer mon compte" reste désactivé tant que les CGU ne sont pas
cochées). Suite complète `go test -tags postgres_integration
./internal/modules/signup/... ./internal/modules/pricing/...
./internal/modules/users/...` contre `staging` — verts à l'exception de
trois échecs confirmés pré-existants et sans rapport avec cette session
(migration 124 — unicité d'e-mail — jamais appliquée sur `staging` ; un
argument de requête manquant dans un test déjà présent avant cette
semaine ; `TestAuthRepository_Postgres`, déjà documenté comme pré-existant
au chantier 8). Aucune régression trouvée sur les chantiers déjà livrés
cette semaine (9/10/12/13) en dehors des changements décrits ci-dessus.

### LOT A Semaine 3 — Chantier 14 : tunnel front (wello-back-office) (2026-09-11)

- **Gap découvert en cours de route** : la session utilisateur (login/
  `GetUserByToken`) n'exposait `auth_provider` nulle part, alors que l'écran
  forcé de définition de mot de passe (dernier point du chantier) en a
  besoin pour décider s'il doit s'afficher. Plutôt que d'ajouter une colonne
  à la requête SQL partagée de 74 colonnes (`scanUserLoginRow`, utilisée par
  chaque requête authentifiée de l'API), nouveau point d'entrée isolé :
  `GET /v1/auth/password/needs-set` (`AuthRepository.NeedsPasswordSet`,
  requête à une seule table) — zéro risque sur le chemin d'auth existant.
  Testé (`TestNeedsPasswordSet_Postgres`, 5 cas) contre `staging`.
- **`AddressAutocomplete.tsx`** (wello-back-office) : `fields` étendu
  (`place_id`, `international_phone_number`, `opening_hours`, `types`,
  `name`) et nouveau prop `searchType` (`'address'` par défaut, inchangé
  pour les deux call sites existants — EstablishmentTab, ProfileTab —
  `'establishment'` pour l'écran 2 du tunnel, seul moyen d'obtenir
  téléphone/horaires/catégorie : Google ne les renvoie jamais pour une
  simple adresse). Nouveau prop `onInputChange`, ajouté après un bug trouvé
  en testant réellement le tunnel dans un navigateur (voir plus bas) : sans
  lui, un texte tapé sans sélectionner de suggestion Google (API
  indisponible, ou établissement non répertorié) ne remontait jamais au
  formulaire parent — seul `onSelect` (déclenché uniquement par une vraie
  sélection) le faisait.
- **`CreateEstablishmentDialog.tsx`** branché sur `AddressAutocomplete`
  (`searchType="address"`, comme demandé — pas de changement de
  comportement pour cet écran interne, seulement moins de ressaisie).
- **Tunnel** (`src/pages/signup-tunnel/`) : trois écrans, état en mémoire
  (`React.useState`, persistance `sessionStorage` en plus pour survivre à un
  rafraîchissement accidentel sur le même appareil). **Écart assumé par
  rapport au brief** : "chaque écran persiste côté serveur à sa validation,
  reprise sur un autre appareil" n'est pas construit — `POST /v1/signup` est
  atomique (un seul appel final, pas de sauvegarde partielle possible côté
  API), et créer un mécanisme de sauvegarde par écran aurait dépassé ce
  chantier. Seul le `context_token` du chantier 11 (une vraie ressource
  serveur) est restauré depuis `?ctx=`.
- **Correspondance NAF → archétype (écran 3, §5.5.1)** : non tirée du
  document de référence (indisponible) — construite depuis
  `merchant_presets.naf_codes` (migration 131). Plusieurs codes sont
  partagés entre archétypes (56.10A : traditional+pizzeria ; 56.10C :
  pizzeria+fast_food+snack) ; l'ordre de priorité retenu en cas
  d'ambiguïté (pizzeria d'abord) est une hypothèse, signalée dans le code
  (`signupTunnel.ts`) comme à vérifier contre le vrai §5.5.1.
- **Écran 1** : bouton Google (Google Identity Services, chargé à la volée,
  aucune dépendance ajoutée) en action principale pleine largeur ; identité
  e-mail/mot de passe repliable, non dégradée visuellement une fois ouverte.
  `VITE_GOOGLE_CLIENT_ID` (nouvelle variable, même valeur que
  `GOOGLE_CLIENT_ID` côté API) doit être configurée avant que le bouton
  fonctionne — sans elle il échoue proprement vers le chemin e-mail, jamais
  un écran cassé (vérifié dans le test navigateur ci-dessous).
- **"Adresse déjà utilisée" (écran 1)** : ne peut être détecté qu'à la
  soumission finale (écran 3) — `/v1/signup` est le seul point qui vérifie
  l'unicité de l'e-mail, il n'existe pas de contrôle de disponibilité
  indépendant. Sur `email_already_used`, l'orchestrateur renvoie
  explicitement à l'écran 1 avec le message et les deux liens demandés.
- **Idempotency-Key** : générée une fois par tunnel (`crypto.randomUUID()`,
  `sessionStorage`), réutilisée sur tout retry — jamais régénérée avant un
  nouveau succès ou un nouvel appel de `/creer-mon-compte`.
- **`publicTunnelApi.ts`** : client dédié, n'envoie jamais `X-App-Source`
  (contrairement à `apiClient` partagé, qui l'ajoute inconditionnellement) —
  confirmé sur le code API que ce header n'a aucun effet sur les routes
  publiques du tunnel de toute façon, mais l'omission reste volontaire et
  documentée en tête de fichier, comme demandé.
- **Testé réellement, pas seulement compilé** : API lancée en local
  (`go run ./cmd/api`) contre `staging`, `npm run dev` pointé dessus,
  parcours complet des 3 écrans piloté par Playwright (chromium, installé
  temporairement, retiré ensuite) dans un vrai navigateur — screenshots à
  chaque étape. C'est cette passe qui a révélé le bug `onInputChange`
  ci-dessus (corrigé avant de considérer le chantier terminé). Soumission
  finale (`POST /v1/signup`) délibérément non déclenchée pendant ce test
  pour ne pas créer un faux marchand sur `staging`. `npx tsc --noEmit` et
  `npx eslint src` verts sur tous les fichiers touchés/créés (le reste des
  74 avertissements/erreurs eslint du dépôt sont préexistants, hors
  périmètre).
- **Confirmations utilisateur déjà obtenues avant ce chantier** : restriction
  par référent HTTP de `VITE_GOOGLE_PLACES_API_KEY` déjà configurée en
  console Google Cloud (confirmé par l'utilisateur) ; SIRET 81490975000014
  déjà résolu manuellement (chantier 10).
- **Non fait, à faire avant mise en production** : configurer
  `VITE_GOOGLE_CLIENT_ID` (Google Cloud Console, OAuth 2.0) en
  staging/production, valeur identique à `GOOGLE_CLIENT_ID` côté API.

### LOT A Semaine 3 — Chantier 12 : résolution de l'entité légale (2026-09-11)

- **Schéma de l'API vérifié en direct** (fetch de son OpenAPI + appels réels
  à `recherche-entreprises.api.gouv.fr`, 2026-09-11), pas deviné : `q`,
  `code_postal`, `activite_principale` (codes NAF pointés, séparés par
  virgules), `etat_administratif` (`A`/`C`). Un résultat porte un `siret` de
  premier niveau **souvent vide** — le SIRET utile vit dans `siege.siret`
  (siège social) ou dans `matching_etablissements[].siret` (l'établissement
  qui correspond réellement au `code_postal` demandé, potentiellement
  différent du siège pour une enseigne à plusieurs adresses). `resolvedEstablishment`
  (`internal/modules/companies/client.go`) préfère un établissement de
  `matching_etablissements` dont le `code_postal` correspond exactement,
  sinon retombe sur `siege`, sinon le premier établissement disponible —
  vérifié en direct sur "Boulangerie Joseph" (75001) : renvoie le bon SIRET
  et la bonne adresse, pas ceux du siège d'une enseigne différente.
- **pg_trgm vérifié NON installé sur `staging`** (`pg_available_extensions`
  le montre installable, mais `pg_extension` ne le liste pas) — conformément
  à l'instruction du chantier ("vérifier... sinon similarité applicative"),
  la similarité de nom est calculée entièrement en Go
  (`internal/modules/companies/similarity.go` : Levenshtein normalisé sur
  texte normalisé — minuscules, accents français retirés, ponctuation
  écrasée), sans dépendance nouvelle. pg_trgm reste une amélioration
  possible si l'extension est installée plus tard ; non bloquant ici.
- **Poids du score = choix d'implémentation, pas une valeur du brief** :
  0,75 × similarité nom + 0,20 × exactitude code postal + 0,05 × bonus
  d'unicité (bonus seulement si l'API ne renvoie qu'un seul résultat brut).
  Les deux seuils (0,85 haute confiance ; 0,5 plancher) sont bien ceux du
  §5.4.2, appliqués tels quels.
- **Défaillance du tiers → jamais une erreur** : `Service.Resolve` ne
  retourne d'erreur Go que pour une entrée invalide ou le débit dépassé —
  toute autre panne (timeout, réseau, statut HTTP inattendu, JSON invalide)
  est capturée et journalée, puis renvoyée comme une liste de candidats
  vide en 200, exactement la bascule "saisie manuelle" attendue par le
  tunnel.
- **Cache Redis** (name normalisé + code postal, 7 jours) et **débit limité
  par IP** (20/heure — valeur non donnée par le brief, choisie plus stricte
  que celle du chantier 11 puisque c'est la seule route publique du tunnel
  qui sollicite un tiers) via le même `redis.Client.TooManyRequestsFromIP`
  que le chantier 11.
- **TVA intracommunautaire (`helpers.ComputeVATNumber`)** : câblée dans
  `POSRepository.InsertMerchant`, donc active pour tout nouveau marchand
  quel que soit le canal (`/v1/signup` et `/pos/create`), pas seulement
  après un `companies/resolve` — un SIRET valide suffit à dériver le SIREN,
  aucune dépendance à cet endpoint. `merchant.vat_number` reste vide (`''`
  → `NULL`) pour un SIRET dont les 9 premiers caractères ne sont pas
  numériques, sans jamais bloquer la création.
- **Exécuté** : `go build ./...` vert ; `go test
  ./internal/modules/companies/... ./internal/helpers/...` verts (7 + 2
  tests, dont un test end-to-end contre l'API réelle exécuté manuellement,
  pas dans la suite automatisée, pour ne pas rendre les tests dépendants
  d'un tiers) ; `go test -tags postgres_integration
  ./internal/modules/signup/... -run TestSignup_Nominal` re-exécuté contre
  `staging` avec une nouvelle assertion `vat_number = "FR44732829320"` —
  vert.

### LOT A Semaine 3 — Chantier 11 : jeton de contexte et tarification (2026-09-11)

- **`pricing_catalog`** (migration 133), une seule table plans+modules+addons
  plutôt qu'une par catégorie — `kind` discrimine. `package_name` (pas un
  `packages.id` figé) pour rester robuste à des id auto-incrémentés
  différents entre environnements ; `pricing.Repository.GetPackageIDByName`
  résout dynamiquement. `annual_price_cents` est le tarif **mensuel**
  équivalent sous engagement annuel (grille "7900 / 6600 annuel"), jamais un
  forfait annuel — nommé ainsi dans le commentaire de colonne pour éviter la
  confusion la plus probable en relecture.
- **"Pro" et "Complet" n'existaient dans `packages` sous aucun nom** —
  décidé avec l'utilisateur de créer deux nouvelles lignes (migration 134)
  plutôt que de réutiliser Standard/Premium (contenu jamais confirmé
  équivalent). `stripe_price_id = ''`, même convention que Deis/Premium
  Waiter/Pointage déjà en production pour un package sans Price Stripe réel.
  **Un vrai Stripe Price doit être créé côté dashboard avant que "Pro" ou
  "Complet" puisse être réellement facturé** — hors de portée (accès
  Stripe dashboard).
- **Formule du pack le moins cher (11b)** — la vraie formule §4.4 était
  indisponible ; confirmée avec l'utilisateur comme une variante de ma
  proposition initiale : Essentiel n'inclut jamais aucun module (exclu dès
  qu'un module est demandé, "0 module inclus" est littéral) ; Complet est
  toujours au prix plat, quel que soit le nombre de modules ; Pro inclut
  gratuitement ses deux modules demandés les plus chers et facture le reste
  individuellement ; à égalité, le pack supérieur gagne. Implémentée une
  seule fois (`pricing.Service.ResolveCheapestPlan`) — site, tunnel et
  back-office (lot B) l'appelleront tous, jamais une réimplémentation.
- **Bornes** : trois paliers confirmés (1ère borne 18900/mois les 24 premiers
  mois puis 8900 ; chaque borne supplémentaire 13900/mois les 24 premiers
  mois puis 8900 aussi) — stockés comme données de référence dans
  `pricing_catalog` (kind='addon') uniquement. Aucune logique de facturation
  ne les consomme dans ce chantier : les bornes ne font pas partie du panier
  "pack le moins cher", et une logique d'amortissement/proration serait un
  chantier à part.
- **`POST /v1/public/signup-context` / `GET .../{token}`** (`internal/modules/signup/`) :
  réutilise `signup_sessions` (déjà prévue à cet effet — voir le commentaire
  de la migration 127) avec `state = 'context'`, `id = context_token` généré
  côté serveur (auto-référentiel, exactement ce que `GetSessionByContextToken`
  attendait déjà). **TTL choisi arbitrairement à 7 jours** (aucune valeur
  donnée par le brief) — plus long que les 24h de rejeu de `/v1/signup` :
  un panier composé sur le site vitrine peut être repris plusieurs jours
  après. Débit limité par IP via un nouveau
  `redis.Client.TooManyRequestsFromIP` généralisé depuis le throttle déjà
  existant de `SendPasswordResetLink` (30/heure/IP — pas de valeur donnée
  non plus, choisie par analogie).
- **11c déjà branché avant ce chantier** : `resolvePackageID`/
  `GetSessionByContextToken` existaient depuis le chantier 6 (prévus pour un
  futur chantier qui écrirait une ligne "contexte" — celui-ci). Rien à
  modifier côté `/v1/signup` lui-même ; seul le producteur de la ligne
  manquait.
- **Exécuté** : `go build ./...` vert ; migrations 133/134 appliquées sur
  `staging` ; `go test -tags postgres_integration
  ./internal/modules/pricing/... ./internal/modules/signup/...` — tous
  verts, incluant un test de bout en bout
  (`TestSignup_ContextTokenResolvesPackage_Postgres`) qui prouve qu'un
  panier à cinq modules résout "complet" et que `/v1/signup` crée bien
  l'abonnement sur ce package, pas sur le défaut Essentiel.

### LOT A Semaine 3 — Chantier 10 : unicité du SIRET (2026-09-11)

- **Migration 132** : `CREATE UNIQUE INDEX CONCURRENTLY uq_merchant_siret_valid
  ON merchant (siret) WHERE siret ~ '^[0-9]{14}$' AND is_active`. Partiel sur
  les deux prédicats — voir l'en-tête du fichier pour le détail (parc
  historique au format invalide non NOT NULL-défaillant, et réactivation
  d'un marchand archivé qui retombe sous la contrainte). Le doublon
  81490975000014 (Le Maghreb / Ok Pizza) a été résolu manuellement par
  l'utilisateur avant l'écriture de cette migration, comme demandé.
- **Pré-contrôle réaligné sur le prédicat de l'index** (point explicitement
  demandé par le chantier) : `signup.merchantSIRETExists` ajoute `AND
  is_active` — sans ce correctif, le pré-contrôle aurait rejeté un SIRET que
  l'index autorise pourtant (celui d'un marchand désormais désactivé),
  puisqu'il comparait `siret = $1` sans filtrer sur l'activité.
- **Repli DB** (`signup.isSIRETUniqueViolation`, `internal/modules/signup/service.go`) :
  intercepte spécifiquement la violation de `uq_merchant_siret_valid`
  (`pgconn.PgError.Code == "23505" && ConstraintName ==
  "uq_merchant_siret_valid"`) dans la transaction de création — jamais une
  violation d'unicité générique, pour ne pas masquer une vraie erreur sous le
  même message. Traduit vers `models.ErrInvalidInput`, identique au message
  du pré-contrôle (§5.7, ne révèle jamais l'existence du compte).
- **Volontairement non fait** (10c) : aucune contrainte `CHECK` sur le
  format, aucune correction/normalisation d'un SIRET existant — voir l'en-tête
  de la migration.
- **Exécuté** : `go build ./...` vert ; migration 132 appliquée sur
  `staging` ; suite `go test -tags postgres_integration
  ./internal/modules/signup/...` (6 tests préexistants + 1 nouveau) —
  7/7 verts après correction d'un test devenu obsolète
  (`TestSignup_Nominal_Postgres` attendait encore 4 catégories pour "snack",
  le compte v1 d'avant le chantier 9 ; passé à 6, le compte v2). Nouveau
  `TestUqMerchantSiretValid_Postgres` prouve le comportement de l'index en
  direct (deux marchands actifs ne peuvent jamais partager un SIRET valide ;
  un SIRET libéré par la désactivation de son porteur redevient utilisable ;
  la réactivation retombe sous la contrainte).

### LOT A Semaine 3 — Chantier 13 : déduction automatique des tâches de démarrage (2026-09-11)

- **`RecomputeOnboarding(ctx, merchantID)`** (`internal/modules/onboarding/service.go`)
  ré-évalue quatre des cinq conditions à chaque appel (jamais un delta) et
  n'écrit que via `SetTaskDoneIfNotDone` (`UPDATE ... WHERE status <> 'done'`)
  — idempotent par construction, donc appelable sans réfléchir depuis
  n'importe quel point d'écriture, y compris plusieurs fois pour le même
  événement.
  - **menu** : `EXISTS` sur `products` avec `enabled = TRUE`, `price > 0`,
    `tva_in_id/tva_delivery_id/tva_take_away_id <> 0`.
  - **device** : `EXISTS` sur `kiosks` pour le marchand (aucune distinction
    de statut — la ligne existe dès l'enrôlement).
  - **team** : `COUNT(*) FROM users_rights WHERE enabled = TRUE >= 2` — le
    propriétaire créé au signup compte comme la première ligne.
  - **logo** : `merchant.logo_url IS NOT NULL AND <> ''`.
  - **payment** : volontairement absent d'ici — `MarkPaymentMandateAccepted`
    existe comme point d'entrée pour le lot B (mandat SEPA), non appelé par
    quoi que ce soit encore, exactement le "poser le hook, ne pas
    l'implémenter" demandé.
- **Points d'écriture branchés**, tous par injection tardive
  (`SetOnboardingService`, appelé une fois dans `cmd/api/routes.go` après
  construction de `onboardingService`) plutôt qu'un nouveau paramètre de
  constructeur — évite de casser la signature de quatre services déjà
  largement utilisés (et leurs tests existants) :
  - `menu.MenuService.onMenuChanged` (déjà appelé après toute mutation
    produit/catégorie — le point de signal "quelque chose a changé au menu"
    existait déjà, pas besoin d'un nouveau call site).
  - `kiosk.Service.EnrollDevice`, après la transaction de création de la
    borne.
  - `pos.POSService.SetLogoURL`, après l'upload du logo.
  - `users.UsersService.CreateUser`, après création d'une ligne
    `users_rights` pour un `merchant_id` non vide (staff ajouté).
- **Skip (`POST /v1/merchants/{id}/onboarding/{code}/skip`)** : migration 135
  ajoute `skip_reason`/`skipped_at` (distincts de `completed_at`, qui reste
  réservé à une vraie complétion). Réservé au propriétaire — vérifié via
  `currentUser.Rights.Admin` (même flag que gèle `signup.createOwnerAndMerchant`
  à la création). Un événement métier réel reste prioritaire sur un skip :
  `RecomputeOnboarding` traite `'skipped'` comme `'pending'` pour la
  promotion vers `'done'` (`status <> 'done'` couvre les deux) — décision non
  explicitée par le brief, mais l'inverse (un skip qui bloque définitivement
  la détection automatique) semblait plus surprenant. Impossible de `skip`
  une tâche déjà `'done'` (`ErrOnboardingTaskAlreadyDone`, 409).
- **Exécuté** : `go build ./...` vert ; migration 135 appliquée sur
  `staging` ; `go test -tags postgres_integration
  ./internal/modules/onboarding/... -v` contre `staging` — 3/3 verts
  (`TestRecomputeOnboarding_Postgres`, `TestSkipTask_Postgres` nouveaux,
  `TestGetOnboarding_Postgres` préexistant toujours vert).

### LOT A Semaine 3 — Chantier 9 : corrections post-revue (2026-09-11)

- **Répartition des zones validée par l'utilisateur** : traditional 14/6
  (inchangé), pizzeria 10 en zone unique (inchangé), **brasserie corrigée**
  13/4/8 (Salle/Comptoir/Terrasse) — la répartition initiale 12/5/8 était une
  hypothèse. Le Comptoir se compte en tabourets (4 postes) : modélisé en
  `table_count: 4, seats: 1, shape: "circle"` (une ligne `locations` par
  tabouret), plutôt que le `seats: 2, shape: square` retenu par erreur au
  premier passage.
- **`cash_handling` confirmé tel quel, logique consignée** :
  `cash_register_required_for_ordering = true` sur les six archétypes — c'est
  ce qui garantit qu'un marchand en `activation_state = 'SETUP'` ne peut
  encaisser aucune commande avant sa mise en exploitation.
  `waiter_app_can_cash_in = true` uniquement pour traditional/brasserie/
  pizzeria (service à table, où un serveur encaisse au guéridon) ; `false`
  pour fast_food/snack/bakery (modèle comptoir, l'encaissement ne se fait
  qu'à la caisse).
- **Immutabilité de 131 une fois livrée** : `ON CONFLICT (code, version) DO
  NOTHING` rend le fichier idempotent (rejouable sans erreur) mais **pas
  réactualisable** — une fois `131_merchant_presets_v2` appliquée quelque
  part, corriger un contenu v2 (ex. une répartition de tables) exige une
  migration v3, jamais une modification de ce fichier suivie d'un rejeu (le
  INSERT n'écrase pas une ligne déjà présente). La correction ci-dessus a été
  faite en modifiant directement `131_merchant_presets_v2.up.sql` uniquement
  parce que ce fichier n'avait pas encore quitté `migrations/todo/` (jamais
  appliqué en production) — plus permis une fois passé en `done/`.
- **Exécuté** : ligne `prst-brasserie-2` corrigée directement sur `staging`
  (déjà appliquée depuis le chantier précédent, avant que cette revue
  n'arrive) puisque non encore livrée ; `go test -tags postgres_integration
  ./internal/modules/presets/... -run TestApplyPreset -v` re-exécuté contre
  `staging` après correction — toujours 4/4 verts (aucun test n'exerçait
  spécifiquement la zone Comptoir de brasserie, donc rien à mettre à jour
  côté tests pour ce point précis).

### LOT A Semaine 3 — Chantier 9 : correction du seed des archétypes v2 (2026-09-10)

- **Contenu v2 fourni par l'utilisateur en réponse à une question de
  clarification** — le brief lui-même contenait un `[COLLER ICI LE TABLEAU
  CI-DESSUS]` non résolu et `docs/parcours-client-v2.docx` n'existe pas dans
  le dépôt. Plutôt que de proposer un nouveau contenu inventé (exactement le
  problème que ce chantier corrige), la table exacte a été redemandée avant
  d'écrire quoi que ce soit.
- **Nouvelle version (v2) plutôt qu'écrasement en place**, comme demandé :
  `UPDATE ... SET is_active = false WHERE version = 1` puis `INSERT` des six
  lignes v2 (`prst-<code>-2`), `ON CONFLICT (code, version) DO NOTHING` pour
  rester idempotent comme 126. `GetActivePresetByCode` (tri par `version
  DESC`, filtre `is_active`) bascule donc automatiquement sur v2 sans
  modification de code.
- **Champ renommé `payments` → `cash_handling`** dans `PresetConfig`
  (`internal/modules/presets/models.go`) et dans le JSON de la migration —
  `PaymentsConfig` devient `CashHandlingConfig`. Seuls
  `internal/modules/presets/{models,repository}.go` référençaient ce champ ;
  aucune autre référence dans le dépôt (vérifié par grep) donc aucun autre
  fichier à toucher.
- **Assomptions documentées ici faute de détail dans la table fournie** (à
  vérifier contre le document de référence si disponible un jour) :
  - Répartition des tables entre zones quand la table donne un total sans
    détail par zone : traditional 20 tables/2 zones → Salle 14 + Terrasse 6 ;
    brasserie 25 tables/3 zones → Salle 12 + Comptoir 5 + Terrasse 8.
    Comptoir/Terrasse repris en `seats: 2, shape: square` (mobilier bar/petite
    table), le reste en `seats: 4, shape: rectangle`, par cohérence avec les
    formes déjà utilisées en v1.
  - `cash_handling` (ex-`payments`) : la table v2 ne redonne pas ces deux
    booléens par archétype — conservés identiques à v1 pour chaque code
    (rien ne les consomme encore, donc aucun risque à date).
  - Libellés/descriptions (`label`, `description`) : reformulés pour rester
    cohérents avec le nouveau contenu (ex. traditional mentionne désormais la
    terrasse) ; la table fournie ne spécifiait que la configuration, pas ces
    deux champs texte.
- **Tests** (`internal/modules/presets/apply_preset_postgres_integration_test.go`) :
  `TestApplyPreset_Snack_CategoriesAndChannels_Postgres` mis à jour
  (`manage_on_site` attendu `true` — c'était le bug ; canaux tous `true`,
  nouvelles catégories, `preparation_time` 10, version figée 2).
  `TestApplyPreset_Traditional_FloorPlan_Postgres` mis à jour pour les deux
  zones (Salle 14 / Terrasse 6, au lieu d'une seule zone de 12).
  `TestApplyPreset_FastFood_NoFloorPlan_Postgres` ajouté (n'existait pas) :
  vérifie explicitement 0 `floors`/0 `locations` et `pager_number_required =
  true`, comme demandé par le chantier.
- **Exécuté** : `go build ./...` (vert) ; migrations 125 à 131 appliquées sur
  `staging` dans l'ordre (aucune n'y était encore appliquée) ; `go test
  -tags postgres_integration ./internal/modules/presets/... -run
  TestApplyPreset -v` contre `staging` — 4/4 verts ; validation
  supplémentaire (`Config.Validate()`) exécutée pour les six archétypes v2
  (traditional, brasserie, pizzeria, fast_food, snack, bakery) via un
  programme jetable, tous valides, aucun ne trouvé en `is_active=false`
  après migration sauf les six lignes v1 attendues.

### LOT A Semaine 2 — Chantier 8 : définition de mot de passe sur compte Google (2026-09-10)

- **`auth_provider` après définition du mot de passe — décidé avec l'utilisateur** :
  `'both'`, pas `'google'` inchangé. Argument retenu : le compte gagne
  réellement un second moyen de connexion opérationnel à cet instant —
  garder `'google'` mentirait sur l'état réel. Effet pratique nul sur la
  logique du chantier 7 : `googleauth.Repository.FindUserByEmail` (la
  vérification "compte sans mot de passe" de `/v1/auth/google`) lit déjà la
  colonne `password` directement (`password != ''`), jamais `auth_provider`
  — ce chantier ne fait donc que garder la colonne honnête pour un futur
  écran ou une requête d'analytique qui s'y fierait.
- **`POST /v1/auth/password/set`**, protégé (`authMiddleware`), self-service
  strict : l'identité vient du jeton (`helpers.ExtractToken` +
  `GetUserByToken`, même mécanique que `SetPIN` dans le même fichier —
  pattern suivi à l'identique plutôt que `middleware.UserFromContext`,
  utilisé ailleurs cette semaine, pour rester cohérent avec le reste de ce
  fichier précis), jamais un `user_id` du corps de la requête.
- **Garde d'éligibilité atomique** (`AuthRepository.SetPasswordForGoogleAccount`) :
  un seul `UPDATE ... WHERE auth_provider = 'google' AND password = ''`,
  `RowsAffected() == 0` distingue "pas un compte Google" et "a déjà un mot
  de passe" — les deux retournent la même erreur
  (`ErrAccountNotEligibleForPasswordSet`, 409), délibérément indifférenciées
  (aucune des deux n'a besoin d'un message plus précis, et ça évite une
  lecture séparée avant l'écriture, donc pas de fenêtre de course entre les
  deux).
- **Politique existante réutilisée telle quelle** :
  `helpers.ValidatePassword`/`helpers.HashUserPassword` (coût 12) — mêmes
  fonctions que le chantier 6, aucune duplication de règle de mot de passe.
- **Déclenchement côté interface (écran forcé au premier accès POS) hors
  périmètre**, comme demandé — seul l'endpoint est livré.
- **Test** (`internal/modules/auth/password_set_postgres_integration_test.go`) :
  compte Google sans mot de passe → succès, `auth_provider` devient `both`,
  hash vérifiable via `helpers.PasswordMatches` ; compte Google avec mot de
  passe déjà défini → rejeté, rien modifié ; compte `auth_provider =
  'password'` → rejeté (cet endpoint est Google-only) ; mot de passe trop
  court → rejeté avant toute écriture. Exercé au niveau `Service`
  (`AuthRepository.SetPasswordForGoogleAccount`'s clause WHERE est ce qui
  compte réellement ici) plutôt qu'au niveau HTTP — la résolution du jeton
  côté handler est déjà couverte par `TestAuthRepository_Postgres`/`GetUserByToken`.
- **Exécuté** : `go build ./...`, `go test -tags postgres_integration
  ./internal/modules/auth/... -run TestSetPasswordForGoogleAccount_Postgres`
  contre Postgres 16 local — vert. `TestAuthRepository_Postgres` (préexistant,
  non touché par ce chantier) échoue sur ce conteneur de dev local
  (`role_permissions_permission_key_fkey` — la migration 100, appliquée
  pendant le rattrapage de la semaine 1, dépréciait `pos.access` du
  catalogue de permissions ; le test le référence encore) — confirmé
  identique sur `staging` (branche) avant toute modification de cette
  semaine (`git stash`), donc sans rapport avec ce chantier.

### LOT A Semaine 2 — Chantier 7 : connexion Google (2026-09-10)

- **Dépendance ajoutée** : `github.com/golang-jwt/jwt/v5` — aucune bibliothèque
  JWT n'existait dans ce dépôt. Nécessaire pour vérifier le `id_token`
  entrant de Google (un vrai JWT signé RS256, côté Google — ne concerne pas
  la contrainte "aucun JWT dans ce système", qui porte sur les jetons émis
  par cette API elle-même, jamais introduite ici : la réponse reste le
  `users_rights.token` opaque existant, confirmé par un test qui compare les
  deux valeurs directement en base).
- **7a. Schéma** : `users.google_sub` ajouté (`migrations/todo/130_users_google_sub.up.sql`).
  `UNIQUE` simple (pas d'index partiel) — en Postgres, une contrainte
  `UNIQUE` ne considère jamais plusieurs `NULL` en conflit, donc tous les
  comptes sans Google restent acceptés sans avoir besoin d'un `WHERE
  google_sub IS NOT NULL` explicite. `users.auth_provider` existait déjà
  (tiré en avance au chantier 6).
- **7b. `POST /v1/auth/google`** (public, nouveau module `internal/modules/googleauth`) :
  - **Vérification du `id_token`** (`verifier.go`) : JWKS Google
    (`https://www.googleapis.com/oauth2/v3/certs`) récupéré et mis en cache
    **1h explicitement** — géré à la main (mutex + horodatage), pas délégué
    au cache interne d'une bibliothèque, pour que la durée soit exacte et
    visible plutôt qu'une politique opaque. Contrôles : signature RS256,
    `aud == GOOGLE_CLIENT_ID` (nouvelle variable d'environnement,
    `internal/config/google.go` — **optionnelle au démarrage**, contrairement
    à `GOOGLE_API_KEY` : `POST /v1/auth/google` échoue simplement à
    l'exécution si elle est absente, même posture que
    `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` selon CLAUDE.md), `iss`, `exp`.
    `email_verified` est retourné tel quel par `Verify` (pas rejeté à ce
    niveau) — le refus est une règle métier tranchée par le service, pas
    une question cryptographique.
  - **Les cinq branches de §5.2.3, implémentées exactement, dans l'ordre** :
    1. `google_sub` connu → connexion. Résolution du token via une nouvelle
       requête (`FindTokenByGoogleSub`), puis **réutilisation intégrale**
       d'`auth.AuthRepository.GetUserByToken` (la même grosse jointure que
       tout autre chemin de session dans cette API) pour construire la
       réponse — rien re-dérivé à la main.
    2. `google_sub` inconnu, adresse inconnue → `google_account_not_found`
       (404). Ce n'est pas le rôle de cet endpoint de créer un compte — le
       message renvoyé invite explicitement vers `POST /v1/signup` avec
       `provider: "google"` (chantier 7c).
    3. `google_sub` inconnu, adresse connue, **sans mot de passe** →
       rattachement automatique (`LinkGoogleSub`) puis connexion.
       "Sans mot de passe" = `password = ''`, pas `IS NULL` :
       `users.password` est `NOT NULL` dans ce schéma (vérifié), donc `''`
       est le sentinel qu'un compte créé côté Google (chantier 7c) porte
       réellement.
    4. `google_sub` inconnu, adresse connue, **avec mot de passe** → refus
       (`google_account_has_password`, 409), **jamais de rattachement
       silencieux** — vérifié par un test qui contrôle `google_sub IS NULL`
       après le refus, pas seulement le code d'erreur.
    5. `email_verified == false` → refus systématique
       (`google_email_not_verified`), contrôlé en premier, avant toute autre
       branche — jamais contournable.
  - Erreurs traduites au niveau du `Handler` (`translateError`), pas dans
    `internal/models` : `models` ne peut pas importer un module métier
    (inverserait le sens des dépendances) — même convention que
    `presets.ErrPresetNotFound` au chantier 6.
- **7c. Extension de `POST /v1/signup`, `provider: "google"`** :
  `signup.Service.Signup` se scinde en `signupPassword`/`signupGoogle`, la
  partie commune (validation SIRET/preset, création marchand via
  `POSService.CreateMerchant`, `ApplyPreset`, `onboarding_tasks`) factorisée
  dans `createOwnerAndMerchant` — aucune duplication de la logique de
  création de marchand, comme pour le chemin mot de passe. `id_token`
  remplace entièrement email + mot de passe (même vérification et même
  garde `email_verified` que `/v1/auth/google`, pour qu'on ne puisse pas
  contourner ce refus en passant par l'inscription avec une adresse non
  contrôlée). Nouvelle méthode `UsersRepository.CreateGoogleUser`
  (`users/create_repository.go`) plutôt qu'un paramètre optionnel sur
  `CreateUser` — l'ensemble de colonnes écrites diffère réellement
  (`auth_provider`, `google_sub`, `email_verified_at = now()`, `password =
  ''`), pas une simple variante.
  **Imprécision mineure assumée** : `CreateGoogleUser` retombe sur
  `models.ErrEmailAlreadyUsed` pour toute violation de contrainte unique
  détectée par `dbx.IsDuplicateEntry` (générique, ne distingue pas
  `uq_users_email_lower` de `uq_users_google_sub`) — en pratique
  inatteignable pour `google_sub` (la branche 1 de `/v1/auth/google` l'aurait
  déjà intercepté avant d'arriver ici), donc non corrigé pour rester
  proportionné à un cas résiduel.
- **Tests** :
  - `internal/modules/googleauth/verifier_test.go` (unitaire, sans DB) :
    signature valide, `aud`/`iss`/`exp` invalides, signature falsifiée
    (signée par une autre clé que celle publiée), `email_verified=false`
    retourné sans être rejeté par `Verify`. Serveur JWKS local
    (`httptest.NewServer`) + clé RSA générée pour le test — `certsURL` rendu
    injectable dans `Verifier` pour ça (`NewVerifierWithCertsURL`, exportée,
    utilisée aussi par le test d'inscription Google — voir plus bas).
  - `internal/modules/googleauth/service_postgres_integration_test.go` :
    les cinq branches, via `authenticateClaims` directement (pas
    `Authenticate`) — la vérification cryptographique est déjà couverte
    séparément, inutile de refaire un vrai `id_token` signé pour tester
    l'arbre de décision base de données. **Piège rencontré** : le `cleanup()`
    d'usage (nettoyer avant de semer) a été appelé APRÈS la création du
    marchand par erreur dans une première version — supprimait le marchand
    qu'on venait de créer. Corrigé (nettoyage en fin de test uniquement,
    commentaire explicite pour ne pas reproduire l'erreur) ; un deuxième
    piège (`GetUserByToken` scanne plusieurs colonnes `merchant_parameters`/
    `scannorder_settings` en `NOT NULL`, absentes d'un simple `INSERT INTO
    merchant`) a nécessité de semer ces deux tables satellites aussi.
  - `internal/modules/signup/handler_postgres_integration_test.go`,
    `TestSignup_GoogleProvider_Postgres` : inscription complète via
    `provider: "google"`, vérifie `auth_provider='google'`,
    `google_sub` renseigné, `password=''`, `email_verified_at` non NULL.
- **Exécuté** : `go build ./...`, `go test ./...` (mêmes 4 échecs
  préexistants et sans rapport), `go test -tags postgres_integration
  ./internal/modules/googleauth/... ./internal/modules/signup/...` contre
  Postgres 16 local — tout vert, suites rejouées (`-count=1`) pour confirmer
  la répétabilité après les deux corrections de test ci-dessus.

### LOT A Semaine 2 — Chantier 6 : signup_sessions, POST /v1/signup, onboarding_tasks (2026-09-10)

- **6a. `signup_sessions`** (`migrations/todo/127_signup_sessions.up.sql`) :
  table à double rôle, non détaillé par le brief — documenté ici. `id` =
  valeur brute de l'en-tête `Idempotency-Key`, pas un id préfixé généré
  (`helpers.GeneratePrefixedID`) : c'est ce qui permet à un rejeu de
  retrouver directement la ligne. `payload` porte d'abord la requête
  entrante (`state = 'pending'`), puis est **écrasé** par la réponse HTTP
  mise en cache une fois le traitement terminé (`state = 'completed'` ou
  `'failed'`) — `{"status": <code>, "body": <bytes exacts écrits par
  models.SendJSON/SendErrorJSON>}`. `context_token` : la colonne existe et
  sa recherche est câblée (`GetSessionByContextToken`), mais **aucun
  endpoint de ce chantier n'écrit de ligne "contexte" séparée** — un futur
  chantier (page de sélection d'offre, etc.) pourra en écrire une que
  `/v1/signup` saura déjà lire.
  - Purge quotidienne à 30 jours (`internal/tasks/signup_sessions.go`,
    `CleanupExpiredSignupSessions`, cron `45 4 * * *`), même modèle que
    `CleanupExpiredPasswordResets`. Distincte de la fenêtre de rejeu 24h
    (`SignupSessionTTL`), qui ne borne que le comportement applicatif
    (`state`), pas la rétention en base.
  - Colonne `users.auth_provider` (`migrations/todo/128_users_auth_provider.up.sql`)
    **tirée en avance du chantier 7a** : le brief la déclare sous "7a.
    Schéma", mais le code du chantier 6b l'écrit dès maintenant
    (`auth_provider = 'password'`). `google_sub` reste au chantier 7.
    Défaut `'password'` — tout utilisateur existant en garde le
    comportement inchangé (ils ont tous un mot de passe).
- **6b. `POST /v1/signup`** (public, `cmd/api/routes.go`, hors
  `authMiddleware` ; nouveau module `internal/modules/signup`) :
  - **`package_id` — décidé avec l'utilisateur** : aucun endpoint de ce
    chantier ne crée de ligne `signup_sessions` "contexte" avant l'appel à
    `/v1/signup`, donc `context_token` ne peut résoudre un `package_id` que
    si un flux amont (hors périmètre) en a déjà stocké un dans son
    `payload`. Défaut retenu : `packages.id=1` ("Essentiel", le seul
    package hors ceux à consonance interne/test à porter un vrai
    `trial_period_days`) — convention self-serve SaaS courante (démarrer
    sur le palier le plus bas, laisser monter en gamme ensuite), un seul
    marchand vivant dessus aujourd'hui donc aucun biais tiré du volume.
  - **Réutilisation complète, aucune duplication** (consigne du chantier) :
    `Service.Signup` appelle `POSService.CreateMerchant` tel quel
    (`InsertMerchant`/`InsertSubscription`/`InitMerchantSatellites`/
    `EnsureSystemRoles`/`SetDefaultRoleID`/lien ADMIN du propriétaire — rien
    de dupliqué), à l'intérieur de la MÊME transaction
    (`dbutils.RunInTx` détecte une transaction déjà active dans le `ctx` —
    `ExtractTx(ctx) != nil` — et exécute directement la closure sans en
    ouvrir une seconde : confirmé en lisant `internal/utils/dbutils/run_in_tx.go`
    avant d'écrire quoi que ce soit). `users.UsersRepository.CreateUser` est
    lui aussi réutilisé tel quel — `name = lower(email)` est simplement
    l'argument `fullName` passé par cet appelant, `auth_provider` vient du
    défaut de colonne (pas besoin d'étendre la signature).
  - **`CreateMerchantResponse` étendu** (`OwnerRightsToken`, champ additif
    `omitempty`) : `/pos/create` existant n'est pas affecté, `/v1/signup` en
    a besoin pour renvoyer "le jeton opaque `users_rights.token`" sans
    requête supplémentaire.
  - **Garde `settings.manage` non contournée** (point d'attention du
    chantier) : elle vit uniquement sur la route `/pos/create`
    (`cmd/api/routes.go`), jamais dans `POSService`. Appeler
    `posService.CreateMerchant` directement depuis `signup.Service` ne
    contourne donc rien — la garde ne s'appliquait déjà qu'à la route, pas
    au service.
  - **SIRET** : format 14 chiffres + clé de Luhn (`helpers.ValidateSIRETFormat`,
    vérifié contre `73282932000074`, le SIRET public de La Poste/INSEE,
    couramment utilisé comme exemple valide). Existence réelle non
    vérifiée (non bloquant, conforme). SIRET déjà pris → rejet générique
    (`models.ErrInvalidInput`, même famille d'erreur qu'un format invalide,
    ne révèle jamais l'adresse du compte existant) + log structuré
    (`zap.String("siret", ...)`) pour traitement manuel.
    **Point d'attention non résolu, à surveiller** : `merchant.siret` n'a
    aucune contrainte d'unicité en base (vérifié). La vérification de ce
    chantier est un pre-check applicatif seulement — une vraie double
    inscription concurrente sur le même SIRET n'est pas interceptée au
    niveau base (contrairement à l'e-mail, qui avait `uq_users_email_lower`
    et son propre chantier dédié, LOT A Semaine 1 Chantier 3). Hors
    périmètre explicite de ce chantier ; signalé plutôt que corrigé en
    silence.
  - **Idempotence** (mécanisme non détaillé par le brief, conçu et
    documenté ici) : le `Handler` capture les octets exacts écrits par
    `models.SendJSON`/`SendErrorJSON` via un petit `http.ResponseWriter`
    maison en mémoire (`memResponseWriter` — délibérément pas
    `net/http/httptest.ResponseRecorder`, réservé aux tests), les met en
    cache (`CompleteSession`/`FailSession`) puis les recopie vers le writer
    réel. `TryBeginSession` (`INSERT ... ON CONFLICT (id) DO NOTHING`) tranche
    la course concurrente : le perdant relit la session et rejoue si
    terminale, renvoie 409 (`signup_in_progress`) si encore `pending`.
    **Sémantique standard de clé d'idempotence retenue** : succès ET rejet
    métier (e-mail pris, SIRET pris, preset invalide...) sont tous les deux
    mis en cache et rejoués à l'identique — une même clé réutilisée après un
    échec ne retraite jamais silencieusement avec des données différentes.
    Le rejeu n'est **pas** garanti identique octet pour octet
    (`signup_sessions.payload` est `jsonb`, tel que spécifié par le
    chantier — Postgres reformate l'espacement JSON au retour), seulement
    identique en contenu ; test adapté en conséquence (comparaison JSON
    décodée, pas `bytes.Equal`).
- **6c. `onboarding_tasks`** (`migrations/todo/129_onboarding_tasks.up.sql`,
  nouveau module `internal/modules/onboarding`) : cinq lignes fixes
  (`menu`, `payment`, `device`, `team`, `logo`), statut `pending` à la
  création — la déduction automatique reste explicitement hors périmètre
  (semaine 3). `GET /v1/merchants/{id}/onboarding` : **protégé**
  (`authMiddleware`), scopé au marchand du jeton appelant — le brief ne
  précisait pas public/protégé ; choisi par cohérence avec le reste de
  l'API ("every authenticated request is scoped to a merchant via the auth
  token", CLAUDE.md), pas un des trois points explicitement soumis à
  confirmation.
- **Tests** (`internal/modules/signup/handler_postgres_integration_test.go`,
  `internal/modules/onboarding/service_postgres_integration_test.go`) : cas
  nominal (catégories/canaux du preset "snack" appliqués, 5 tâches
  d'onboarding créées, jeton retourné = `users_rights.token` réel),
  e-mail déjà utilisé, SIRET déjà pris (+ vérifie qu'aucune adresse n'est
  révélée dans la réponse), rejeu idempotent (même contenu, aucun
  doublon créé), `Idempotency-Key` manquant, lecture onboarding scopée au
  marchand appelant. Tous exécutés via le `Handler` réel (pas seulement le
  `Service`) — c'est lui qui porte la mécanique d'idempotence. **Piège
  rencontré en écrivant ces tests** : les lignes `signup_sessions` ne sont
  pas nettoyées par défaut entre deux exécutions de la suite (chaque test
  utilise une clé d'idempotence fixe) — un rejeu résiduel d'un run
  précédent pointait vers un user/merchant déjà supprimé par le nettoyage
  précédent. Corrigé (`DELETE FROM signup_sessions WHERE email = ...` ajouté
  au nettoyage de test) ; suite rejouée trois fois de suite (`-count=1`)
  pour confirmer.
- **Exécuté** : `go build ./...`, `go test ./...` (mêmes 4 échecs
  préexistants et sans rapport que les chantiers précédents), `go test -tags
  postgres_integration ./internal/modules/signup/... ./internal/modules/onboarding/...`
  contre Postgres 16 local — tout vert.

### LOT A Semaine 2 — Chantier 5 : table des archétypes et ApplyPreset (2026-09-10)

- **5a. Schéma** (`migrations/todo/125_merchant_presets.up.sql`) : table
  `merchant_presets` (id `prst-...`, `UNIQUE (code, version)`, `config jsonb`)
  + colonnes `merchant.preset_code/preset_version/place_id/activation_state
  (défaut 'SETUP')/went_live_at/signup_channel/signup_source`. **Rattrapage
  explicite inclus** (le point signalé par le chantier) : `UPDATE merchant
  SET activation_state = 'LIVE', went_live_at = COALESCE(went_live_at,
  creation_date) WHERE activation_state = 'SETUP'` — testé contre le
  Postgres de dev local, 28 marchands existants correctement rebasculés en
  LIVE après l'`ALTER TABLE`. Pas de `CHECK` sur `activation_state` :
  cohérent avec `orders.state`/`brand_status`, aucun statut de ce dépôt n'a
  de contrainte au niveau base, validation applicative seulement.
- **5b. Correspondance config ↔ colonnes, confirmée avant d'écrire le JSON**
  (comme demandé) — contre le schéma réel vérifié sur staging, pas le dump
  statique `docs/migration-postgres/04-schema-postgres-target.sql` (déjà
  périmé sur au moins une colonne, `pos_covers_count_required`, ajoutée par
  la migration 086 et absente du dump) :
  - `channels` → `merchant_parameters.manage_on_site/manage_take_away/manage_delivery`
  - `kitchen.display` → `production_display_mode` ; `kitchen.call_numbers` →
    `pager_number_required`
  - `categories` → lignes `productcateg` (réutilise
    `menu.MenuRepository.CreateProductCategory`, pas de SQL dupliqué)
  - `floor_plan` (zones, tables) → lignes `floors` + `locations` (réutilise
    `locations.LocationsRepository.CreateFloor`/`CreateTable`)
  - `prep_times` → `preparation_time_mode/preparation_time/minimum_preparation_time/maximum_preparation_time`
  - `covers_required` → `pos_covers_count_required`
  - **Quatre champs sans correspondance propre, tranchés par l'utilisateur** :
    `kitchen.grouping` retiré du schéma (le seul candidat réel,
    `production_profiles`, est une entité à part entière — capacité/file —
    pas un simple booléen de préréglage) ; `payments` conservé mais mappé sur
    `cash_register_required_for_ordering`/`waiter_app_can_cash_in` (aucun
    réglage "moyens de paiement acceptés" n'existe dans ce schéma) ;
    `printing` retiré (aucune colonne marchand — `printers` est une table
    d'instances avec IP/port/bluetooth, pas préréglable) ; `suggested_modules`
    conservé mais **jamais appliqué** par `ApplyPreset` — stocké/informatif
    seulement, `packages`/`subscriptions` restent pilotés par le
    souscripteur, jamais par un archétype.
- **5b, contenu des six archétypes — réserve explicite** : `docs/parcours-client-v2.docx`
  n'est pas accessible depuis cet environnement (fichier absent du dépôt).
  Labels/catégories/tailles de plan de salle/temps de préparation/codes NAF
  du seed (`migrations/todo/126_merchant_presets_seed.up.sql`) sont **une
  proposition, pas une extraction du document de référence** — décision
  utilisateur de les garder tels quels pour débloquer ApplyPreset, à
  corriger après coup contre le vrai §5.5.2. `ON CONFLICT (code, version) DO
  NOTHING`, même convention que 095_roles_permissions_catalog. `naf_codes`
  volontairement non lu par le Go de ce chantier (voir plus bas).
- **5c. `ApplyPreset`** (nouveau module `internal/modules/presets` :
  `models.go`/`repository.go`/`service.go`) :
  1. `GetActivePresetByCode` — la **dernière** version active du code (pas
     une version figée à l'avance) : `merchant.preset_version` fige ensuite
     ce qui a été appliqué, jamais rétroactif si l'archétype évolue ensuite.
  2. `PresetConfig.Validate()` — rejette AVANT toute écriture (format
     `kitchen.display`, `prep_times.mode`, cohérence min ≤ max, formes de
     table valides `circle/square/rectangle/oval` — même liste que
     `locations/service.go`'s `validTableShapes`, dupliquée plutôt
     qu'exportée pour ne pas élargir la surface publique de `locations` pour
     un seul appelant). Empêche exactement ce que le chantier demandait :
     qu'une erreur de préréglage produise un marchand à moitié configuré.
  3. `UpdateMerchantParametersFromConfig` (UPDATE unique) → catégories
     (`menu.MenuRepository.CreateProductCategory`, dans l'ordre) → zones +
     tables si `floor_plan.enabled` (`locations.LocationsRepository`,
     positionnement en grille simple — un préréglage ne peut pas connaître
     une vraie disposition de salle) → `SetMerchantPreset` (dernière étape,
     une fois tout le reste réussi).
  4. Tout passe par `dbx.GetDB(ctx, ...)` (pattern de transaction ambiante
     déjà utilisé par `dbutils.RunInTx`/`POSService.CreateMerchant`) : appelé
     depuis une transaction existante, chaque sous-repository (menu,
     locations, presets) y participe automatiquement sans qu'`ApplyPreset`
     ait besoin de connaître ou propager un `*sql.Tx`.
  5. **`naf_codes` (`text[]`)** délibérément non modélisé côté Go dans ce
     chantier : aucun appelant n'a besoin de le relire (ApplyPreset ne
     l'utilise pas — "non exploité par ce chantier", cf. le commentaire de
     la colonne), et ce dépôt n'a aucun précédent de scan d'un tableau
     Postgres via `database/sql`+pgx (aurait exigé `pgtype.Array[string]`,
     une première). Seule la migration seed l'écrit (littéral SQL). À
     traiter proprement si/quand un futur endpoint doit l'exposer.
  6. **`POSService.CreateMerchant` (`/pos/create`) non modifié** — la
     consigne du chantier ("ApplyPreset appelée DANS la transaction de
     création, après InitMerchantSatellites") décrit l'usage prévu, câblé au
     chantier 6 (`POST /v1/signup`) ; `/pos/create` ne prend aujourd'hui
     aucun `presetCode` et rien dans ce chantier ne demandait de lui en
     ajouter un.
- **Tests** (`internal/modules/presets/apply_preset_postgres_integration_test.go`,
  package externe `presets_test` pour réutiliser `pos.NewPOSRepository` sans
  risque de cycle) : `snack` (cas nominal demandé — catégories dans l'ordre,
  canaux, `floor_plan` désactivé → aucune ligne `floors`/`locations`,
  `preset_code`/`preset_version` figés) ; `traditional` (branche plan de
  salle — 1 zone, 12 tables de 4) ; code inconnu →
  `presets.ErrPresetNotFound`, rien écrit sur `merchant`. **Les trois
  passent** contre le Postgres 16 de dev local — qui a nécessité un
  rattrapage supplémentaire, migration 086 (`pos_covers_count_required`),
  plus ancienne que tout ce qui avait été rattrapé la semaine 1 et donc
  manquée par ce rattrapage-là ; déjà dans `migrations/done/` (donc
  réellement appliquée en production), aucune conséquence hors de ce
  conteneur jetable.
- **Exécuté** : `go build ./...`, `go test ./...` (mêmes 4 échecs
  préexistants et sans rapport que les chantiers précédents), `go test -tags
  postgres_integration ./internal/modules/presets/...` contre Postgres 16
  local — tout vert.

### LOT A Semaine 2 — Prérequis : neutralisation de la migration 113 (2026-09-10)

Vérification demandée avant de commencer la semaine 2 ("la migration 113 doit
être neutralisée... confirmer que c'est fait"). **Ne l'était pas** :
`113_drop_users_rights_admin_column` avait été déplacée de `migrations/todo/`
vers `migrations/done/` par le commit `c113a46` ("onboarding LOT A"), dans le
même mouvement que 087/094-123 — sauf que son propre en-tête dit explicitement
"PREPARED, NOT MEANT TO BE APPLIED YET" : trois lecteurs de
`users_rights.admin` sont toujours actifs dans le code déployé
(`auth/permissions.go` `Has()` branche historique, `UserLoginRow.HasAdminRole()`,
`LoginLegacyFields.Admin` — confirmé inchangé dans le code actuel).

- **Vérifié directement contre staging** (lecture seule) avant toute action :
  `users_rights.admin` existe toujours, `roles`/`permissions` existent déjà
  (094+ bien appliquées) — la migration 113 elle-même n'a donc PAS été
  exécutée, aucun incident réel. Le problème est le classement
  (`migrations/done/` = signal "déjà appliqué, rien à vérifier"), pas l'état
  de la base.
- **Décision utilisateur** : remise dans `migrations/todo/` (`git mv`,
  historique préservé) ET neutralisation du contenu — le `DROP COLUMN` de
  `up.sql` est remplacé par un no-op (`RAISE NOTICE` explicite), le corps
  original conservé en commentaire pour restauration verbatim une fois les
  trois lecteurs réellement partis du code déployé (production comprise).
  Objectif : même un « rejouer tous les fichiers de todo/ dans l'ordre, sans
  réfléchir » ne peut plus casser la production. `down.sql` inchangé dans son
  comportement (`ADD COLUMN IF NOT EXISTS` était déjà un no-op sûr si la
  colonne existe), un commentaire ajouté pour expliquer pourquoi il devient
  inatteignable en pratique.
- **Testé contre le Postgres 16 de dev local** : le fichier neutralisé
  s'exécute proprement (le `NOTICE` s'affiche, `users_rights.admin` reste
  intact après coup).
- Chantiers 5 à 8 de cette semaine peuvent commencer.

### LOT A Semaine 1 — Chantier 4 : garde de création + rôle par défaut + sélecteur de rôle (2026-09-10)

Prérequis au self-onboarding (décision N6). Trois changements indissociables
(cf. consigne : livrer un seul des trois cassait soit la création
d'établissement, soit rendait impossible la création d'un second
administrateur) + un correctif de sécurité trouvé en l'implémentant.

- **1. `POST /pos/create`** (`cmd/api/routes.go`) : ajout de
  `middleware.RequirePermission(permission.SettingsManage)`, comme sa
  voisine `/pos/link-user` (`StaffManage`). **N6 confirmée sans adaptation** :
  `RequirePermission` lit `middleware.GetUser(r)`, un unique
  `*auth.UserLoginRow` déjà résolu pour LE marchand du jeton en cours
  (`GetUserByToken`, jointure fixée par le token) — il n'y a nulle part de
  liste de rattachements multi-marchands à ce stade, donc `user.Has(key)`
  évalue structurellement sur le marchand courant, jamais sur l'ensemble des
  rattachements de l'utilisateur. Rien à changer dans `RequirePermission`
  lui-même.
- **2. `POSService.CreateMerchant`** (`internal/modules/pos/create_service.go`) :
  `SetDefaultRoleID` pointe désormais sur `staffRoleID` (deuxième valeur de
  retour d'`EnsureSystemRoles`) au lieu d'`adminRoleID`. Le propriétaire
  (`req.UserID` + `req.Admin`) continue de recevoir explicitement
  `adminRoleID` sur son propre `users_rights` — ce n'était déjà pas lié au
  défaut du marchand dans le code existant (l'appel à `insertUserRightsTx`
  passait déjà `adminRoleID` en argument littéral, pas une lecture du
  default), donc aucun changement nécessaire à cette ligne.
- **Correctif de sécurité trouvé en implémentant le point 3** : `role_id`
  (nouveau champ de `CreateUserRequest`) atteint directement
  `users_rights.role_id` sans qu'aucune vérification n'existe que ce rôle
  appartient bien au marchand cible — un `role_id` d'un AUTRE marchand aurait
  été accepté tel quel. `roles.Service.SetUserRole` (utilisé par l'onglet
  "Droits" existant) fait déjà cette vérification pour le même genre
  d'entrée (`getMerchantRole`, scoping `merchant_id`) : même garde ajoutée
  ici via `UsersRepository.RoleBelongsToMerchant` (dupliquée plutôt
  qu'importée — `internal/modules/roles` importe déjà `internal/modules/users`
  pour `GetUsersRightsToken`, RBAC lot 6, donc l'import inverse créerait un
  cycle). Rejette avec `models.ErrRoleNotFound` (déjà mappé sur 404, code
  existant du module `roles`) avant toute écriture.
- **3. Sélecteur de rôle** :
  - **API** — `CreateUserRequest.RoleID` (nouveau, optionnel) ; s'il est
    fourni et valide (voir garde ci-dessus), il prend le pas sur
    `merchant.default_role_id` dans `UpsertMerchantUserRights` (branche
    INSERT seulement — une ré-activation de lien existant ne touche jamais
    `role_id`, comme avant). `GET /roles` expose maintenant `is_default`
    (bool, `RoleListItem`) : le rôle qui correspond à
    `merchant.default_role_id` du marchand courant — pas de second
    endpoint, `CreateMemberSheet.tsx` réutilise exactement la même requête
    (`rolesApi.list()`, même clé de cache `qk.roles.list()`) qu'`AccessTab.tsx`.
  - **Back-office** — sélecteur de rôle ajouté dans `CreateMemberSheet.tsx`,
    section "Accès" (au-dessus des switches Administrateur/Connexion
    activée, pas en remplacement). Valeur par défaut = le rôle marqué
    `is_default`, mais seulement tant que l'admin n'a pas choisi autre chose
    explicitement (un rafraîchissement de la liste des rôles ne doit pas
    écraser un choix manuel — état `roleIdTouched`). Alimente
    `CreateUserRequest.role_id`.
  - **Champ Planning "Rôle" renommé "Poste RH"** (même fichier,
    `CreateForm`) : c'est `employees.role` (employee/manager/admin), sans
    aucun rapport avec le RBAC — seul le libellé et la variable locale
    (`role` → `hrRole`) changent, la clé JSON (`planning.role`) et le
    comportement restent identiques.
- **4. Régression `Header.tsx`** : le bouton "Nouvel établissement" (et son
  séparateur) n'apparaît plus dans le sélecteur d'établissement que si
  `checkPermission(authData, 'settings.manage')` — un utilisateur "staff"
  (rôle par défaut désormais) ne le voit plus.
- **Vérifié** :
  - `go build ./...`, `go test ./...` : verts (mêmes 4 échecs préexistants
    sans rapport, déjà signalés aux chantiers 1-3).
  - `go test -tags postgres_integration ./internal/modules/pos ./internal/modules/users ./internal/modules/roles/...`
    contre Postgres 16 de dev local, **rattrapé jusqu'à la migration 123
    inclue** (`roles`/`permissions`/`role_permissions` n'existaient pas du
    tout dans ce conteneur avant ce chantier — nécessaires pour tester quoi
    que ce soit RBAC). Une migration dans ce lot de rattrapage
    (`113_drop_users_rights_admin_column`) a été **annulée juste après**
    (son `.down.sql`) : le code actuel lit/écrit encore activement
    `users_rights.admin` (ex. `Login`), donc cette migration 113 ne peut pas
    être déployée nulle part en l'état — l'avoir laissée active aurait
    faussé la suite des tests locaux. N'affecte que ce conteneur Docker
    jetable, aucune conséquence sur staging/production.
  - Deux nouveaux tests permanents : `TestPOSService_CreateMerchant_DefaultRoleIsStaff_Postgres`
    (`internal/modules/pos`, défaut marchand = "staff", propriétaire reste
    "admin") et `TestUsersService_CreateUser_RoleIDOverride_Postgres`
    (`internal/modules/users`, package externe `users_test` pour la même
    raison de cycle qu'au-dessus — role_id honoré sur le bon marchand,
    rejeté sur un autre). Les deux passent contre Postgres 16 local.
  - `npx tsc --noEmit -p tsconfig.app.json` (wello-back-office) :
    **122 erreurs avant et après** ce chantier (comparaison par `git stash`
    ciblé sur les 3 fichiers touchés) — aucune nouvelle erreur introduite ;
    les 122 sont préexistantes et sans rapport (dette TypeScript déjà
    présente sur `staging`, hors périmètre). `npm run build` (Vite) réussit.
- **Scénario de test manuel** (à rejouer dans le back-office) :
  1. Connecté en administrateur (rôle "admin", ou tout rôle avec
     `settings.manage`) : le sélecteur d'établissement (Header) affiche
     "Nouvel établissement" ; l'onglet Équipe → "Ajouter un membre" →
     l'onglet "Nouveau membre" affiche un champ "Rôle" (section Accès,
     au-dessus d'Administrateur/Connexion activée) présélectionné sur le
     rôle par défaut du marchand (libellé suffixé "(défaut)"), et un champ
     séparé "Poste RH" dans la section Planning (employee/manager/admin,
     optionnel). Créer un membre sans toucher au sélecteur de rôle → le
     nouveau membre reçoit le rôle par défaut du marchand ("staff" pour un
     établissement créé après ce chantier). Changer explicitement le
     sélecteur avant de créer → le membre reçoit le rôle choisi.
  2. Connecté avec un utilisateur "staff" (aucune permission
     `settings.manage`) : le bouton "Nouvel établissement" a disparu du
     sélecteur d'établissement (seule la liste des établissements existants
     reste visible). Un appel direct à `POST /pos/create` avec ce jeton
     renvoie 403 (`access_denied`).
  3. Sur un marchand existant créé avant ce chantier (default_role_id
     encore "admin", non rétroactif) : créer un membre sans toucher au
     sélecteur de rôle continue de lui donner le rôle "admin" de CE
     marchand — le changement ne s'applique qu'aux marchands créés après ce
     chantier, comme prévu (pas de migration de données ici).

### LOT A Semaine 1 — Chantier 3 : unicité de l'adresse électronique (2026-09-10)

Prérequis au self-onboarding (décision N7 ; docs/parcours-client-v2.docx §5.3).
Avant ce chantier : aucune contrainte d'unicité sur `users.email` (ni en base,
ni en code) — `CreateUser` n'interrogeait jamais la table avant d'insérer, et
le login comparait `UPPER(u.email) = UPPER(?)`, incompatible avec un index
fonctionnel `lower(email)`.

- **Détection de doublons sur staging (exécutée avant d'écrire la migration,
  comme demandé)** : 47 utilisateurs, aucun e-mail NULL/vide, **1 groupe de 4
  doublons** — `iliesbellaltemp@gmail.com` partagé par les user_id 227, 233,
  242, 244, tous `enabled = false`, créés à la même seconde (2026-05-30
  18:40:54), lecture manifeste de comptes de test jetables (pas de doublon
  parmi des comptes actifs). Signalé avant de continuer, conformément à la
  consigne. **Aucun doublon en production** (déjà vérifié en amont de ce
  chantier).
- **Décision utilisateur sur la résolution du doublon** : pas d'index partiel
  (`WHERE enabled = TRUE`), pas de nettoyage manuel hors-migration — la
  migration elle-même désambiguïse tout doublon normalisé restant en
  ajoutant un suffixe `+1`, `+2`, ... sur la partie locale de l'adresse
  (compte le plus ancien par `user_id` inchangé, les suivants suffixés) :
  no-op strict quand il n'y a aucun doublon (donc no-op en production),
  self-healing partout ailleurs (staging comme un futur environnement dans
  le même état). `users.name` est resynchronisé sur l'e-mail (voir
  ci-dessous) **après** cette désambiguïsation, pour ne pas propager le
  doublon dans `uq_users_name`.
- **Migration** `migrations/todo/124_users_email_unique_index.{up,down}.sql` :
  1. `email = lower(trim(email))` : normalisation.
  2. Désambiguïsation des doublons restants (ci-dessus), no-op en prod.
  3. `name = lower(trim(email))` : `users.name` devient l'adresse (préparation
     au retrait de la colonne, lot C — non fait ici, colonne conservée
     intacte comme demandé). Exécuté après les étapes 1-2 : `email` est déjà
     normalisé et unique à ce stade, donc aucune collision possible sur
     `uq_users_name` (index déjà existant) — point 4 du chantier vérifié.
  4. `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_users_email_lower ON
     users (lower(email))`, hors bloc transactionnel (même convention que
     087/109/118) ; `.down.sql` symétrique (`DROP INDEX CONCURRENTLY`), sans
     tenter de revenir sur la normalisation des données (même convention que
     117 : une correction de données n'est pas réversible).
  **Testée de bout en bout contre le Postgres 16 de dev local**, qui avait
  lui aussi un doublon réel (5 comptes sur `iliesbellal@gmail.com`) : up
  désambiguïse en `iliesbellal@gmail.com` / `+1` / `+2` / `+3` / `+4`,
  `name` suit, index créé (`Index Scan` confirmé, voir plus bas) ; down
  supprime l'index proprement ; ré-application de up idempotente (0 ligne
  touchée sur les étapes 2-3 la seconde fois, `CREATE INDEX ... IF NOT
  EXISTS` no-op).
- **`internal/modules/auth/repository.go`, fonction `Login`** : le prédicat
  e-mail passe de `UPPER(u.email)=UPPER(?)` à `lower(u.email)=lower(?)`.
  Plan d'exécution vérifié contre le Postgres de dev : `EXPLAIN` sur le seul
  prédicat email isolé confirme `Index Scan using uq_users_email_lower`
  (contre `Seq Scan` avec l'ancien `UPPER(...)`, qui ne peut structurellement
  jamais utiliser cet index, quelle que soit la volumétrie). Sur la requête
  `Login` complète (3 conditions `OR` sur name/email/token, jointe à
  `users_rights`), le planneur choisit un `Seq Scan` + `Hash Join` quelle
  que soit la forme du prédicat — attendu sur ces deux tables (46/55 lignes
  en dev) : Postgres ne descend pas dans un index pour un `OR` de conditions
  disjointes sur des tables aussi petites. Confirmation fonctionnelle
  demandée par le chantier : connexion testée avec une adresse en casse
  mixte (`IliesBellal+1@Gmail.com`) contre une ligne stockée en minuscules —
  correspond bien.
  **Non touché** : `GetUserForPasswordReset` (même fichier) garde
  `UPPER(u.email) = UPPER(?)` — son propre commentaire le documente comme
  « a deliberate copy of the one in Login », qui n'est donc plus tout à fait
  vrai après ce chantier. Non modifié car hors périmètre explicite (la
  consigne ne cite que `Login`) ; à surveiller/aligner si un chantier futur
  retouche ce fichier.
- **`internal/modules/users/create_service.go` (`CreateUser`) et
  `create_repository.go`** : vérification d'existence
  (`UsersRepository.EmailExists`, `lower(email)=lower(?)`) ajoutée juste
  après la validation des champs obligatoires et avant le hachage du mot de
  passe (échec rapide) — retourne `models.ErrEmailAlreadyUsed` (nouveau,
  `internal/models/responses_models.go`, mappé sur 409 comme les autres
  erreurs `...AlreadyExists` du fichier). Repli pour le cas concurrent :
  `CreateUser` (repository) intercepte désormais la violation de
  `uq_users_email_lower` via `dbx.IsDuplicateEntry` (helper déjà existant,
  utilisé ailleurs dans `tags`/`printers`/`productionprofiles`) et la
  traduit dans la même erreur métier plutôt que de laisser remonter l'erreur
  SQL brute.
  **Point d'attention non résolu, à trancher si besoin** : l'index est
  `lower(email)`, sans `trim` ; `EmailExists`/`CreateUser` ne trim pas non
  plus l'entrée. Un e-mail avec espace(s) parasite(s) en entrée
  (`" foo@bar.com"`) ne collisionnerait ni avec le pré-contrôle ni avec
  l'index si `"foo@bar.com"` existe déjà sans espace — la normalisation par
  `trim` n'a été appliquée qu'une fois, historiquement, par la migration.
  Documenté (pas corrigé, hors périmètre explicite de ce chantier) dans le
  nouveau test d'intégration.
  **Autre point d'attention, pas un problème introduit par ce chantier**  :
  `CreateUser` (service) fixe toujours `name = prénom + " " + nom` pour tout
  nouvel utilisateur — seul le historique migré par 124 a `name = email`.
  L'invariant « name suit l'e-mail » établi par la migration s'érode donc
  dès la première création de compte après ce lot ; sans conséquence sur
  `uq_users_name` (les deux valeurs restent uniques indépendamment), mais à
  garder en tête pour le lot C (retrait de la colonne).
- **Test permanent** `TestUsersRepository_EmailUniqueness_Postgres`
  (`internal/modules/users/postgres_integration_test.go`) : `EmailExists`
  case-insensitive avant/après création, `CreateUser` sur un doublon (casse
  différente) rejeté avec `models.ErrEmailAlreadyUsed`, aucune ligne
  insérée. **Exécuté avec succès** contre le Postgres 16 de dev local.
- **Exécuté** : `go build ./...`, `go test ./...`, `go test -tags
  postgres_integration ./internal/modules/users/... ./internal/modules/auth/...`
  contre Postgres 16 local. Tout vert pour ce qui touche à ce chantier.
  Échecs preexistants et sans rapport, confirmés identiques sur `staging`
  avant modification (`git stash`) : trois tests dans
  `internal/modules/auth` (`pin_test.go`, sqlmock) qui n'ont échoué que
  lorsque `DB_DIALECT=postgres` était positionné globalement dans l'environnement
  du process de test plutôt que scopé par test via `pgtest.Open`/`t.Setenv`
  — artefact d'invocation de test, pas un bug ; `TestUsersRepository_Postgres`,
  `TestInsertUserRights_FailsExplicitlyWhenNoDefaultRole` et
  `TestAuthRepository_Postgres` — la base de dev locale n'a pas la table
  `roles` (RBAC), en retard sur des migrations non rattrapées par ce
  chantier (hors périmètre : ne concernent pas `users.email`).

### LOT A Semaine 1 — Chantier 2 : chaînage fiscal sur `DenyOrderLocal` (2026-09-10)

Prérequis au self-onboarding (décision N7, option A ; docs/audit-parcours-onboarding.md
§4 fait marquant #2). Des trois fonctions qui font passer une commande à
`state = 'CLOSED'` (`SetDeliveredLocal`, `DeleteOrderLocal`, `DenyOrderLocal`),
seule `DenyOrderLocal` n'écrivait jamais `hash`/`previous_hash`/`signature` —
une commande refusée par le marchand sortait de la chaîne fiscale `orders`
sans laisser de trace, alors qu'elle atteint bien `state = 'CLOSED'`.

- **Divergence `previous_hash` entre les deux fonctions existantes**,
  signalée avant d'écrire le code (question posée, réponse reçue) : en
  l'absence de commande `CLOSED` précédente pour le marchand,
  `SetDeliveredLocal` écrit `prevHash.String` (chaîne vide `""`) alors que
  `DeleteOrderLocal` écrit `prevHash` (le `sql.NullString`, donc SQL `NULL`)
  — deux représentations différentes du même cas « premier maillon », sans
  marqueur explicite dans ni l'une ni l'autre. **Aucune des deux n'a été
  retenue pour `DenyOrderLocal`** (conforme à la consigne : ne pas harmoniser
  les deux existantes dans ce chantier, ni les imiter dans leur ambiguïté).
  À la place : un marqueur littéral **`"GENESIS_HASH"`**, sur le modèle exact
  déjà en place pour la chaîne `cash_registers`
  (`internal/modules/cash_registers/repository.go`, `actualPrevHash`) —
  seule chaîne du dépôt qui rend ce cas explicite plutôt qu'ambigu. Choix
  utilisateur, avec un principe supplémentaire par rapport à
  `cash_registers` : `"GENESIS_HASH"` est aussi injecté dans le **payload
  haché** (`payload := fmt.Sprintf("%s|%s|%d|%s", actualPrevHash, ...)`), pas
  seulement dans la colonne — la valeur stockée et la valeur hachée restent
  donc toujours identiques pour `DenyOrderLocal`, ce qui n'est le cas ni de
  `SetDeliveredLocal` ni de `DeleteOrderLocal` (toutes deux hachent
  `prevHash.String` mais stockent des choses différentes dans la colonne).
  Ce chantier ne touche pas au code existant de ces deux fonctions.
- **`currentPrice`/`merchantID` manquants dans la signature de
  `DenyOrderLocal`** : sa signature (`orderID, deletionReasonID, comment,
  userID`) suffisait déjà — un `SELECT merchant_id, price FROM orders WHERE
  order_id = ?` a été ajouté en tête de fonction, sur le modèle du bloc « 1)
  Get metadata » déjà présent dans `SetDeliveredLocal`/`DeleteOrderLocal`.
  Aucun changement de signature, donc aucun changement dans `service.go`
  (`SetOrderDenied`/`DenyOrder`) — l'option "remonter depuis l'appelant"
  évoquée par le chantier n'était pas nécessaire.
- **Point d'attention non explicitement listé par le chantier, mais
  nécessaire à la correction du chaînage** : la requête qui retrouve le
  hash précédent (`SELECT hash FROM orders WHERE merchant_id = ? AND state =
  'CLOSED' ORDER BY delivered_on DESC, order_id DESC LIMIT 1 FOR UPDATE`,
  partagée par les trois fonctions) trie par `delivered_on DESC`. En
  PostgreSQL, `NULL` est trié en premier en `DESC` — si `DenyOrderLocal`
  n'écrivait pas `delivered_on`, chaque commande refusée resterait
  définitivement en tête de ce tri pour son marchand (NULL prime sur toute
  date réelle), cassant l'ordre du chaînage pour toutes les clôtures
  suivantes dès le premier refus. `delivered_on = `+dbx.UTCNow()+`` a donc
  été ajouté à l'`UPDATE`, dans le même bloc que les trois colonnes de
  chaînage — même valeur d'horodatage de clôture que `DeleteOrderLocal`
  écrit déjà pour la même raison (elle aussi une clôture qui n'est pas une
  vraie livraison).
- **Payload identique dans sa forme** aux deux fonctions existantes :
  `fmt.Sprintf("%s|%s|%d|%s", <prevHash>, deliveredOn, currentPrice,
  orderID)`, `sha256`, `security.SignHash`.
- **Non touché, conforme à la consigne** : aucune reprise d'historique — les
  lignes `DENIED` déjà en base restent `NULL` sur les trois colonnes ; aucune
  liste d'exclusion des rapports fiscaux modifiée.
- **Test permanent** (`deny_order_fiscal_chain_postgres_integration_test.go`,
  build tag `postgres_integration`) : deux refus successifs pour le même
  marchand — le premier vérifie `hash`/`signature` non nuls et
  `previous_hash = "GENESIS_HASH"`, le second vérifie que `previous_hash`
  vaut bien le `hash` du premier (chaînage effectif, pas seulement présence
  de valeurs). **Exécuté avec succès** contre le Postgres 16 de dev local
  (`docker-compose.postgres.yml`, conteneur `welloresto-postgres-dev`) — la
  base locale était en retard de plusieurs migrations (jusqu'à la 123 ;
  115_permission_reports_staff_performance_read a échoué faute de table
  `permissions`, sans rapport avec ce chantier, ignorée) : rattrapée pour
  pouvoir exécuter ce test et le reste de la suite `postgres_integration` du
  module.
- **Exécuté** : `go build ./...`, `go test ./...`,
  `go test -tags postgres_integration ./internal/modules/order_life_cycle/...
  ./internal/modules/cash_registers/... ./internal/modules/receipt/...`
  contre Postgres 16 local. Tout vert pour ce qui touche à ce chantier. Trois
  échecs preexistants et sans rapport, confirmés identiques sur `staging`
  avant modification (`git stash`) : schéma de la base de dev locale encore
  en retard sur deux tests (`delivery_travel_seconds` manquante,
  `configurable_attribute_options` de mauvais type — nécessitent des
  migrations au-delà de celles rattrapées ici) et un test sqlmock
  (`TestSendInvoiceByEmail_NewEmail_CreatesCustomer`) dont l'échec vient d'un
  ordre de colonnes non déterministe côté mock, pas du code testé.

### LOT A Semaine 1 — Chantier 1 : validation de `FISCAL_SIGNING_KEY` au démarrage (2026-09-10)

Prérequis au self-onboarding (docs/audit-parcours-onboarding.md, défaut #9) :
`security.SignHash` (internal/utils/security/hash_signing.go) lit
`FISCAL_SIGNING_KEY` via `os.Getenv` sans aucune validation — absente, elle
produit silencieusement une signature HMAC avec une clé vide, ni erreur ni
log, donc reproductible par n'importe qui.

- **Périmètre exact** : ajout d'un contrôle de démarrage, sur le modèle exact
  de `PIN_PEPPER` (`internal/config/config.go`) — champ `App.FiscalSigningKey`
  chargé via `os.Getenv("FISCAL_SIGNING_KEY")` dans `Load()`, `log.Fatal
  ("FISCAL_SIGNING_KEY is not set")` dans `validate()` si vide. Aucune autre
  modification.
- **`security.SignHash` non touchée** — signature et comportement identiques
  (consigne explicite du chantier). Le contrôle vit uniquement dans
  `internal/config`, au démarrage de l'application (`config.Load()`), pas
  dans `SignHash` elle-même.
- **Production non impactée** : la variable y est déjà définie (vérifié avant
  ce chantier) — ce contrôle ne fait donc rien de nouveau en prod, il ferme
  simplement la possibilité de déployer sans elle.
- **Tests d'intégration Postgres** (`*_postgres_integration_test.go`,
  build tag `postgres_integration`) : `SetDeliveredLocal`/`DeleteOrderLocal`/
  `DenyOrderLocal` (order_life_cycle) et les tests de `cash_registers`/
  `receipt` appellent `security.SignHash` indirectement. Aucun ne définissait
  `FISCAL_SIGNING_KEY`. Comme `SignHash` ne valide toujours rien elle-même,
  cela ne faisait pas échouer ces tests — mais pour rester cohérent avec le
  comportement réel de l'application (où `config.Load()` refuserait de
  démarrer sans cette variable), `FISCAL_SIGNING_KEY` est désormais posée par
  `pgtest.Open` (`internal/database/dbx/pgtest/pgtest.go`), le point de
  setup commun à tous ces tests — même schéma que `DB_DIALECT` qui y est déjà
  fixé. Valeur de test : `itest-fiscal-signing-key`, seulement si la variable
  n'est pas déjà présente dans l'environnement.
- **Exécuté** : `go build ./...` et `go test ./...` — verts. Les échecs
  observés sur `internal/modules/planning/leave`, `internal/modules/planning/
  swaps` et `internal/modules/ubereats` sont préexistants et sans rapport
  avec ce chantier (confirmés identiques sur `staging` avant modification,
  via `git stash`).

### PROMPT 25 Phase 3/4 — Les 7 autres fusions retenues (2026-09-07)

Suite du point de passage sur l'onglet CA. Même méthode partout : requête
fusionnée en repository.go/cancellations.go/upsell.go/products.go/
discounts.go, câblée dans service.go, vérifiée en lecture seule contre
staging (établissement 212, ancien chemin vs nouveau) avant d'écrire le test
permanent.

- **Commandes** (`GetOrdersTotalsThreePeriods`) : 3→1, mêmes 5 agrégats
  (dont les couverts, `FILTER` imbriqué `fenêtre AND places_settings>0`), pas
  de jointure. Nouveau motif réutilisé pour toutes les fusions simples de ce
  lot : `xxxSelectFragment(w PeriodWindow)` retourne le fragment SQL d'une
  fenêtre et ses arguments dans l'ordre textuel exact — un seul endroit où
  compter "combien de fois `expr` apparaît = combien de fois `exprArgs` est
  répété", plutôt que de recompter à la main à chaque fusion.
- **Règlements** (`GetPaymentsTotalsThreePeriods`) : 3→1, `payments ⋈ orders`,
  `payments.enabled = TRUE` inchangé.
- **TVA** (`GetVATTotalsThreePeriods`) : 3→1. Les deux branches `UNION ALL`
  (lignes produit + frais de livraison) exposent maintenant `creation_date`
  pour que l'agrégat externe puisse la `FILTER`er — seule vraie modification
  structurelle de cette fusion, le reste reprend `GetVATTotals` à l'identique.
- **Annulations** : `GetOrdersCreatedCountThreePeriods` +
  `GetCancellationsTotalsThreePeriods` remplacent les 6 requêtes de
  `cancellationsPeriodTotals` (2 requêtes × 3 périodes) par 2 requêtes au
  total — pas 1, parce que ce sont deux scopes différents
  (`AnalyticsAllOrdersCreatedScope` vs `AnalyticsCancellationsScope`, voir
  scope.go) qui ne partagent pas de FROM commun.
- **Upsell** : la fusion la plus rentable après CA. `GetUpsellTotals` et
  `GetOrdersWithUpsellCount` lisaient déjà la même jointure lourde
  (`orderitems⋈orders⋈products⋈tva_categories`, `is_upsell=true`) pour deux
  agrégats différents — `GetUpsellTotalsWithOrdersTwoPeriods` les fusionne
  ET les étend sur 2 périodes en une seule requête (4→1, jointure lourde
  comprise). `GetUpsellOrdersTotalTwoPeriods` fusionne séparément le
  dénominateur (requête `orders` seule, scope différent) sur 2 périodes
  (2→1). Total : 6→2.
- **Produits** (`GetProductsScopeTotalsTwoPeriods`) : 2→1, 7 agrégats,
  jointure `htLineJoins` inchangée.
- **Remises** : `GetDiscountsScopeTotalsTwoPeriods` (2→1, `discount_redemptions
  ⋈ orders`) + `GetDiscountsOrdersTotalsTwoPeriods` (2→1, `orders` seule) —
  4→2, même logique de séparation qu'Annulations (deux scopes distincts).
- **Options — fusion écartée**, contrairement au tableau de la Phase 2.
  Trouvé en l'implémentant, pas avant : `GetOptionsScopeTotals` lit
  `optionsCombinedCTE`, une CTE à 5 étages (`scoped_orders`→`scoped_items`→
  `scoped_configs`/`scoped_withouts` matérialisés→`combined`) spécifiquement
  réglée (mesure PROMPT 17 §4 : 2041 ms non-matérialisé vs 438 ms matérialisé,
  seule mesure de durée qui existe réellement dans ce module). La fusionner
  sur 2 périodes aurait exigé de faire remonter `creation_date` à travers les
  5 étages pour un seul gain (2 requêtes → 1) sur une requête déjà optimisée
  — le rapport gain/lisibilité/risque ne tient pas, contrairement aux 7
  fusions ci-dessus. Conforme à la Phase 2 : "une requête illisible coûte
  cher en maintenance", "il n'est pas demandé de tout fusionner". Le code
  actuel (`GetOptionsScopeTotals`, 2 requêtes current/previous) reste
  inchangé.
- **Vérifié contre staging, en lecture seule** (établissement 212, ancien
  chemin vs nouveau) pour les 10 requêtes fusionnées ci-dessus (7 onglets,
  Upsell comptant double) : **résultats identiques dans tous les cas**,
  script jetable (`cmd/analytics_verify_tmp`) supprimé après usage.
- **Tests permanents** (5 nouveaux fichiers `*_multi_period_postgres_integration_test.go`,
  un par groupe d'onglets) : établissement/données dédiés par test, borne
  `[start, end)` vérifiée à l'identique de l'onglet CA, comparaison au
  chemin non fusionné ET à des valeurs attendues codées en dur. **Non
  exécutés dans cet environnement** (pas de Docker) — `go build`/`go vet
  -tags postgres_integration` passent, à faire tourner contre le Postgres 16
  de dev local avant merge.
- **`service.go`** : les 7 onglets câblés sur leurs requêtes fusionnées ;
  `cancellationsPeriodTotals`/`upsellPeriodTotals`/`discountsPeriodTotals`
  (les helpers qui tournaient une fois par période) remplacés par leurs
  pendants `...ThreePeriods`/`...TwoPeriods` qui tournent une fois pour
  toutes les périodes.
- **Exécuté** : `go build ./...`, `go vet ./internal/modules/analytics/...`,
  `go vet -tags postgres_integration ./internal/modules/analytics/...`,
  `go test -count=1 ./internal/modules/analytics/...` — tous verts. Rien
  touché hors `internal/modules/analytics/`. Pas de mesure de durée
  (`analytics_bench --mode=grid` avant/après reste à lancer par le porteur
  du produit, hors de cet environnement).
- **Bilan des requêtes par onglet, comptage seul (pas de durée)** :

  | Onglet | Avant | Après | Requêtes économisées |
  |---|---:|---:|---:|
  | CA | 10 | 5 | -5 |
  | Commandes | 7 | 5 | -2 |
  | Règlements | 7 | 5 | -2 |
  | TVA | 7 | 5 | -2 |
  | Annulations (agrégat) | 11 | 7 | -4 |
  | Options | 9 | 9 | 0 (écartée) |
  | Clients (agrégat) | 4 | 4* | 0 (déjà minimal, cache Phase 0 sur la duplication inter-endpoint) |
  | Upsell (agrégat) | 10 | 6 | -4 |
  | Remises | 9 | 7 | -2 |
  | Produits | 7 | 6 | -1 |

  \* Clients/Annulations/Upsell/Clients-top/Cancellations-by-staff/
  Upsell-by-staff bénéficient en plus du cache Phase 0 (scope+fuseau, et pour
  Clients/Upsell la déduplication `GetCustomersLifetimeStats`/
  `GetUpsellInstrumentationActive`), non reflété dans ce tableau qui ne
  compte que les requêtes SQL déclenchées par l'endpoint lui-même.

### PROMPT 25 Phase 3/4 — Fusion de l'onglet CA : six requêtes de totaux → une (2026-09-07)

- **`PeriodWindow` + trois `*ScopeMultiPeriod`** (`scope.go`) : primitive
  partagée pour toutes les fusions à venir de ce lot. `periodFilterPredicate`
  construit `alias.creation_date >= ? AND alias.creation_date < ?` (alias
  paramétrable — nécessaire pour `GetRevenueTotalsThreePeriods`, dont la
  requête interne lit `creation_date` via un alias différent selon la CTE) ;
  `multiPeriodOrWhere` assemble N fenêtres en `(f1) OR (f2) OR (f3)` —
  **délibérément un OR de petites plages, jamais une plage unique couvrant
  leur union** : l'écart entre la période précédente et l'année passée peut
  couvrir près d'un an, et scanner cet écart aurait annulé le bénéfice de la
  fusion. `AnalyticsOrdersScopeMultiPeriod`/`AnalyticsCancellationsScopeMultiPeriod`/
  `AnalyticsAllOrdersCreatedScopeMultiPeriod` sont les pendants multi-fenêtres
  des trois portées existantes (`scope.go`) — les deux dernières ne sont pas
  encore utilisées (préparées pour la fusion Annulations à suivre).
- **`GetRevenueTotalsThreePeriods`** (`repository.go`) remplace les 3 appels
  `GetRevenueTotalsTTC` + 3 appels `GetRevenueTotalsHT` (6 requêtes) par une
  seule. Point de correction important trouvé en écrivant la requête, pas
  après : TTC **doit** rester `SUM(orders.price)` — jamais une somme des
  lignes `orderitems` recalculée dans la même requête que le HT — parce que
  `orders.price` inclut les frais de livraison et `orderitems` non (même
  écart que celui documenté pour la TVA, `deliveryFeeHTExpr`). Une jointure
  plate `orders ⋈ orderitems` aurait aussi compté TTC/nombre de commandes une
  fois par ligne au lieu d'une fois par commande. D'où la structure à deux
  CTE : `scoped_orders` (une ligne par commande — TTC/nombre lus à la bonne
  granularité) `LEFT JOIN order_ht` (une ligne par commande, HT pré-sommé sur
  ses propres lignes) — jamais une jointure directe. `order_ht` somme la
  valeur brute (non arrondie) par commande, l'agrégat externe re-somme par
  fenêtre puis arrondit une seule fois (`roundToIntExpr`) — somme associative,
  donc rigoureusement le même calcul que l'ancien "somme d'abord, arrondi
  une fois", juste en un aller-retour au lieu de trois. `includeHT=false`
  garde le chemin `orders` seul, sans jointure (3 requêtes → 1, jamais la
  jointure lourde).
- **Vérifié contre staging, en lecture seule, avant d'écrire le test
  permanent** : script Go jetable (`cmd/analytics_verify_tmp`, supprimé après
  usage, jamais commité — même discipline que la vérification du spill
  Phase 5/PROMPT 24) comparant l'ancien chemin (6 appels) au nouveau (1 appel)
  sur l'établissement 212, quatre scénarios (`include_ht` vrai/faux ×
  fenêtres ordinaires/fenêtres à cheval sur la bascule d'heure d'été du
  2026-03-29) — **résultats identiques au centime et à la commande près dans
  les quatre cas**.
- **Test permanent** :
  `revenue_multi_period_postgres_integration_test.go`
  (`TestGetRevenueTotalsThreePeriods_Postgres`) — établissement/produit/TVA
  dédiés, une commande par fenêtre (courante/précédente/année passée), plus
  deux commandes aux bornes exactes de la fenêtre courante (une pile à `End`,
  qui doit être exclue — intervalle semi-ouvert — une pile à `Start`, qui doit
  être incluse) pour verrouiller la sémantique `[start, end)` après la fusion
  OR, plus une fenêtre à cheval sur le 2026-03-29/30 (même date que le bug de
  timeline déjà rencontré une fois dans ce paquet). Compare le chemin fusionné
  à l'ancien chemin (doit être rigoureusement égal) ET à des valeurs
  attendues codées en dur (pour ne pas valider un bug présent identiquement
  des deux côtés). **Non exécuté dans cet environnement** (pas de Docker
  disponible ici) — `go build`/`go vet` passent avec `-tags
  postgres_integration`, à faire tourner contre le Postgres 16 de dev local
  (`docker-compose.postgres.yml`) avant merge.
- **`GetRevenue`** (`service.go`) : les 6 appels remplacés par un seul appel à
  `GetRevenueTotalsThreePeriods`.
- **Exécuté** : `go build ./...`, `go vet ./internal/modules/analytics/...`
  et `go vet -tags postgres_integration ./internal/modules/analytics/...`,
  `go test ./internal/modules/analytics/...` — tous verts. Pas de mesure de
  durée (voir Phase 0/Phase 1 : `analytics_bench --mode=grid` avant/après,
  hors de cet environnement).
- **Point de passage** : les 7 autres fusions retenues en Phase 2
  (Commandes, Règlements, TVA, Annulations, Upsell, Produits/Options/Remises)
  suivent le même motif (`PeriodWindow` + `*ScopeMultiPeriod` déjà posés pour
  Annulations) — à enchaîner sans check-in intermédiaire une fois ce point de
  passage validé, comme convenu.

### PROMPT 25 Phase 0 — Cache du périmètre et du fuseau (2026-09-06)

- **Constat de la Phase 1 (inventaire)** : sur les ~93 requêtes déclenchées par
  un chargement des 10 onglets, 26 (28%) étaient `ResolveAccessibleMerchants`
  (14 appels — 13 onglets + le sélecteur `/analytics/merchants`) et
  `GetMerchantTimezone` (13 appels) — systématiques, même sur un hit du cache
  de réponse complète existant (`s.redis`, `AnalyticsCacheTTL`), puisque le
  périmètre doit être connu avant de pouvoir construire la clé de ce
  cache-là. Aucune des deux ne touche de SQL financier — pas de test de
  non-régression sur des montants nécessaire, pas de bascule d'heure d'été à
  vérifier.
- **`resolveAccessibleMerchants`** (`service.go`) : cache Redis par token brut
  (`models.AnalyticsCachePrefix + "scope:" + token`), TTL
  `accessibleMerchantsCacheTTL = models.UserCacheTTL` — **littéralement la
  même constante**, pas une valeur dupliquée, pour qu'un futur changement du
  TTL de `UserLoginRow` (`internal/modules/auth`) ne puisse pas être oublié
  ici. Remplace les 14 appels directs à
  `Repository.ResolveAccessibleMerchants`.
  - **Précaution sécurité, vérifiée avant d'écrire le code** :
    `auth.AuthService.InvalidateUserCache` existe mais n'est appelé nulle
    part dans le dépôt (`grep` exhaustif, 2026-09-06) — un retrait de droit
    aujourd'hui ne s'appuie déjà que sur l'expiration passive à 60 minutes de
    `UserCacheTTL`, rien ne pousse d'invalidation immédiate. Ce cache-ci ne
    fait donc que **s'aligner** sur la fraîcheur réelle déjà en vigueur, il ne
    la dégrade pas. `internal/modules/auth` reste hors du périmètre de ce
    lot — pas de câblage d'invalidation ajouté ici. Si
    `InvalidateUserCache` est un jour effectivement appelé (un autre lot), il
    suffira d'y ajouter la suppression de `accessibleMerchantsCacheKey(token)`
    — même famille de clé, même token.
- **`getMerchantTimezone`** (`service.go`) : cache Redis par `merchant_id`,
  TTL 24h (`merchantTimezoneCacheTTL`) — pas une donnée de sécurité, pas de
  couplage d'invalidation requis. Remplace les 13 appels directs à
  `Repository.GetMerchantTimezone`.
- **Les deux doublons inter-endpoints écartés en Phase 2** (fusion refusée
  car les deux endpoints de chaque paire sont derrière des permissions
  différentes) se résolvent par le même mécanisme, sans toucher au contrat :
  - `getCustomersLifetimeStatsCached` : `GetClients` et `GetClientsTop`
    appellent `Repository.GetCustomersLifetimeStats` (le scan le plus lourd
    de l'onglet Clients, non borné par période) avec des paramètres
    identiques lors d'un même chargement d'onglet — cache Redis 5 minutes
    (`AnalyticsCacheTTL`, le TTL déjà en usage pour les réponses complètes).
  - `getUpsellInstrumentationActiveCached` : même traitement pour
    `Repository.GetUpsellInstrumentationActive`, appelée par `GetUpsell` et
    `GetUpsellByStaff`.
- **Tests** (`cache_test.go`) : propriété tri-indépendant/périmètre-sensible
  pour les deux nouveaux constructeurs de clé (même forme que les 5 existants
  déjà couverts), plus `TestAccessibleMerchantsCacheTTL_MatchesUserCacheTTL`
  qui verrouille l'égalité littérale des deux constantes — c'est ce test qui
  détecterait une régression future remplaçant la référence par une valeur
  dupliquée à la main. Pas de test comportemental Redis (hit/miss réel) :
  `Service.redis` est un `*redisclient.Client` concret, pas une interface —
  même limite que le reste du fichier, où seuls les constructeurs de clé
  purs sont testés (voir les 5 tests déjà présents dans `cache_test.go`).
- **Exécuté** : `go build ./...`, `go vet ./internal/modules/analytics/...`,
  `go test ./internal/modules/analytics/...` — tous verts. Pas de mesure de
  durée (voir la Phase 1 : `analytics_bench --mode=grid` sera lancé
  avant/après par le porteur du produit, pas depuis cet environnement).
- **Suite** : fusion de l'onglet CA (6 requêtes de totaux → 1) avec son test
  de non-régression, point de passage, puis les 7 autres fusions retenues en
  Phase 2 sans check-in intermédiaire si le motif tient.

### PROMPT 26 Phase 3/4 — Rattrapage et garde-fou anti-redérive (2026-09-07)

- **Phase 3 — `cmd/backfill_customer_stats`** : recalcule les 3 colonnes depuis `orders` sous la
  définition de Phase 1, et stamp `orders.customer_stats_counted_at` sur chaque commande actuellement
  qualifiante (posé si absent, levé sinon) — sans cette étape, le marqueur d'idempotence de Phase 2
  resterait NULL sur tout l'historique et une annulation/réouverture future d'une commande antérieure au
  rattrapage ne déclencherait aucun retrait (`ReverseOrderFromCustomerStats` est un no-op si le marqueur
  n'est pas posé).
  - Idempotent et par lots (500 clients/lot, une petite transaction par client — jamais un verrou long
    sur `customer`), reprenable via `--start-after=<customer_id>`. Mode simulation par défaut
    (`--dry-run` implicite, `--apply` pour écrire), suivant l'instruction du prompt de toujours lancer la
    simulation d'abord.
  - Pas de flags de calibrage supplémentaires (taille de lot, pause) : l'utilisateur a précisé que le run
    de production se ferait de nuit sans activité, donc pas de contention à ménager — la seule
    justification restante pour le lot de 500 est d'éviter une transaction unique sur 26 000 lignes,
    pas la concurrence.
- **Simulation sur staging (26 224 clients)** : 6458 clients corrigés (24,6 %) — cohérent avec l'ordre de
  grandeur du 29 % cité pour PROD (périmètre différent, donc pourcentage non identique attendu).
  Somme des écarts absolus : 3171 commandes, 59 962,92 € de `customer_total_spent`. Exécuté en 13 s en
  dry-run.
- **Apply exécuté sur staging** (mutation additive/idempotente, staging est prévu pour ça — voir
  [[reference_staging_db_access]]) : mêmes chiffres (6458 changés), **11 min 52 s** pour 26 224 clients
  avec l'écriture réelle (transaction courte par client, aller-retour réseau vers Render). Un second
  passage en dry-run juste après confirme `changed=0` sur les 26 224 — le recalcul est stable, la
  correction ne se redéfait pas elle-même.
- **Commande et créneau pour la production** : même commande, `--apply` sans `--start-after` pour un
  premier passage complet. Durée attendue à l'échelle de la volumétrie PROD (26 224 sur staging → 12 min) :
  à budgéter par prudence sur tout le créneau creux 3h-5h documenté dans CLAUDE.md, PROD ayant plus de
  clients et d'historique de commandes que staging. Si interrompu, relancer avec
  `--start-after=<dernier customer_id affiché>` plutôt que reprendre à zéro.
- **Phase 4 — `TasksManager.ReconcileCustomerStats`** (`internal/tasks/customer_stats.go`), cron quotidien
  `30 3 * * *` (`cmd/api/tasks.go`, entre `RecomputeUpsellPatterns` à 3h et `CleanupExpiredPasswordResets`
  à 4h30) :
  - Échantillon de 500 clients (`CustomersRepository.SampleCustomerStatsDrift`, `ORDER BY random()`),
    comparés à un recalcul live sous la même définition canonique — jamais de correction depuis ce cron,
    seulement une détection (le rattrapage reste un geste délibéré, `cmd/backfill_customer_stats`).
  - **Trace exploitable** (migration `122_customer_stats_reconciliation_runs`) : une ligne par exécution
    (taille d'échantillon, mismatches, ratio, écarts max, seuil, alerte, durée) — l'infra cron de ce dépôt
    n'a aucun journal d'exécution propre (CLAUDE.md), donc sans cette table la tâche serait aussi
    invisible que les autres.
  - Seuil d'alerte : 1 % du échantillon. Choix délibérément strict — juste après Phase 3 la dérive réelle
    est nulle, donc tout écart non-bruit mérite un log niveau Error. Aucun canal d'alerte externe
    (Slack/pager) n'existe dans ce dépôt pour une tâche de fond : le log Error + la ligne persistée sont
    le plafond honnête de ce que cette tâche peut faire seule.
- **Vérifié** : `go build ./...` propre sur les paquets modifiés, `go vet` propre sur
  `customers`/`order_life_cycle`/`tasks`/`cmd/backfill_customer_stats` (un vet pré-existant sans rapport
  dans `cmd/api/routes.go`, module `auth`, n'est pas de ce lot). Nouveau test
  `TestStatsReconciliation_Postgres` (client sain non signalé, client à dérive simulée détecté avec
  l'écart exact, ligne persistée relue) et migration 122 appliquée sur staging — tous verts.

### PROMPT 26 Phase 1/2 — Réparer les compteurs client : diagnostic et écriture (2026-09-06)

- **Un seul écrivain, deux points d'appel** : `CustomersRepository.UpdateLoyaltyFromOrder`
  ([repository.go:1454](../internal/modules/customers/repository.go#L1454)) est le seul code qui touche
  `customer_nb_orders`/`customer_total_spent`/`last_order_date`, appelé uniquement depuis `SetDelivered`
  et `SetDeliveredExternal` (`order_life_cycle/service.go`) — donc déjà un point de passage unique pour
  tous les canaux de clôture (POS, ScanNOrder, kiosk via `delivery_sessions`, Uber Eats). L'hypothèse
  « seul le POS est instrumenté » est fausse.
- **4 causes confirmées, vérifiées contre le code et contre staging (26 224 clients,
  `RENDER_STAGING_DATABASE_URL`)** :
  1. **Annulation après clôture jamais décrémentée** (dominante) — `DeleteOrderLocal`
     ([repository.go:774](../internal/modules/order_life_cycle/repository.go#L774)) écrit
     `state='CLOSED', brand_status='CANCELED'` sans aucune garde sur l'état courant ; `SetOrderDeleted`
     n'appelle jamais `OrderStillOpen` avant `DeleteOrder`. Preuve staging : 394 commandes
     CANCELED/DELETED avec `delivered_on` renseigné, et sur le périmètre strict WELLO_RESTO-only,
     3323 clients sur-comptés contre 351 sous-comptés — signature d'un défaut de décrémentation.
  2. **Réouverture + reclôture double-compte** — `ReopenClosedOrder` remet `state='OPEN'` sans condition ;
     `ProcessOrderLoyalty` n'a aucune garde d'idempotence sur le crédit brut, contrairement à la
     progression fidélité juste en dessous (`customer_loyalty_progress_order`). Preuve staging :
     62 réouvertures dans `audit_logs`, 39 commandes closes plus d'une fois. Même mécanisme pour
     `SetDeliveredExternal`, qui ne vérifie délibérément pas `OrderStillOpen` (webhook de confirmation
     de livraison pouvant arriver après une clôture manuelle).
  3. **Filtre de marque trop strict** — `qGetOrder` filtrait `AND o.brand = 'WELLO_RESTO'` : une commande
     Uber Eats/Deliveroo clôturée ne comptait jamais, même avec un `customer_id`. 4036 clients (15,4 %)
     ont au moins une commande qualifiante hors WELLO_RESTO sur staging.
  4. **Échec silencieux** — `ProcessOrderLoyalty` logue l'erreur d'`UpdateLoyaltyFromOrder` mais retourne
     toujours `nil` (priorité assumée : le cycle de vie de la commande prime sur la fidélité). Explique
     plausiblement le résidu de sous-comptage. Non modifié dans ce lot — le contrôle périodique (Phase 4)
     couvrira ce résidu quelle que soit son origine exacte.
- **Définition retenue, validée par l'utilisateur** : reprise du périmètre canonique déjà en place côté
  analytics (`AnalyticsOrdersScope`, `internal/modules/analytics/scope.go`) — compte toute commande
  `state IN ('CLOSED','DONE')` et `upper(brand_status) NOT IN ('DELETED','CANCELED')`, **tous canaux
  confondus** (changement de comportement assumé : ~15 % des clients verront leur `customer_nb_orders`
  augmenter dès le rattrapage). DENIED exclu de fait (jamais CLOSED/DONE). Montant = `o.price`, comme
  partout ailleurs. `last_order_date` = `o.creation_date` de la commande (comme
  `analytics.GetCustomersLifetimeStats`), plus jamais `now()` au moment de la clôture.
- **Mécanisme retenu : un marqueur d'idempotence sur `orders`**, pas un simple re-scope des requêtes
  existantes. Migration additive `121_customer_stats_counted_marker` ajoute
  `orders.customer_stats_counted_at timestamptz` (NULL = ne contribue à aucun compteur actuellement).
  `CustomersRepository.ApplyOrderToCustomerStats`/`ReverseOrderFromCustomerStats`
  ([repository.go:1381](../internal/modules/customers/repository.go#L1381)) sont deux requêtes atomiques
  (CTE `UPDATE ... RETURNING` + `UPDATE ... FROM`) : le crédit/retrait et la pose/levée du marqueur se
  font en un seul aller-retour, sans fenêtre lecture-puis-écriture. `last_order_date` n'est jamais
  décrémenté par arithmétique côté retrait : recalculé par `MAX(creation_date)` sur les commandes encore
  marquées comptées, pour réapparaître correctement sur la commande précédente si la plus récente est
  annulée. Postgres uniquement (`dbx.ActiveDialect() != Postgres` → no-op), conforme à CLAUDE.md
  (MySQL n'est plus une cible live, pas de nouvelle logique MySQL).
- **Câblage des 3 points d'écriture** :
  - `UpdateLoyaltyFromOrder` : `qGetOrder` ne filtre plus par marque (compteurs cross-canal) ; le filtre
    `brand == 'WELLO_RESTO'` est réappliqué juste avant la boucle des programmes de fidélité, qui reste
    inchangée (question de périmètre produit distincte, hors sujet de ce lot).
  - `DeleteOrderLocal` : appelle `ReverseOrderFromCustomerStats` après le passage en CANCELED — couvre
    aussi les annulations marketplace initiées par un staff (`UberEatsService.CancelOrder`/
    `DeliverooService.CancelOrder` sont uniquement appelées en aval de `DeleteOrder`, jamais comme point
    d'entrée indépendant — vérifié par recherche exhaustive des appelants).
  - `ReopenClosedOrder` (repository) : appelle `ReverseOrderFromCustomerStats` avant de remettre
    `state='OPEN'`, pour que la reclôture recrédite proprement avec un prix éventuellement corrigé.
  - Chemins volontairement non touchés : le webhook Uber Eats `event_order_canceled.go` (`CancelOrder`,
    `WHERE state = 'OPEN'`) ne peut jamais annuler une commande déjà comptée ; le webhook Deliveroo
    `UpdateOrderRejected` (DENIED, refus à l'intake) n'atteint jamais un état compté.
- **Vérifié** : `go build ./...`, suite `postgres_integration` de `customers` et `order_life_cycle`
  contre staging — `TestCustomersRepository_Postgres` (idempotence étendue aux compteurs, pas seulement
  à la progression fidélité) et le nouveau `TestCustomerStats_Postgres` (cross-canal, double-clôture,
  annulation après livraison avec recalcul de `last_order_date`, réouverture + correction de prix) tous
  verts. Un échec préexistant sans rapport (`TestOrderLifeCycleRepository_Postgres`, colonne
  `brand_store_id` de la migration 111 non appliquée sur staging) n'est pas de ce lot.
- **Reste à faire** : Phase 3 (script de rattrapage, idempotent, par lots, mode simulation d'abord) et
  Phase 4 (contrôle périodique anti-redérive) — séquencées avec check-in utilisateur entre chaque, comme
  demandé.

### PROMPT 24 Phase 5 — Pool analytique : 2→4 connexions, work_mem 16→8 Mo (2026-09-06)

- **`internal/database/postgres.go`** : `AnalyticsMaxOpenConns` 2→4,
  `AnalyticsWorkMemMB` 16→8, dans le même commit — le calcul du prompt tient
  exactement : 4 connexions × ~4 nœuds de tri × 8 Mo = 128 Mo, + 64 Mo de
  `shared_buffers` = 192 Mo, **identique** au budget actuel (2×4×16 = 128 Mo
  + 64 Mo = 192 Mo). Deux fois plus de connexions concurrentes pour le même
  plafond mémoire, pas une hausse de mémoire.
- **Mesure du spill, pas supposée** : un test Go temporaire (supprimé après
  usage, jamais commité) réutilise verbatim les fonctions de construction de
  requête déjà en production (`AnalyticsOrdersScope`, `htLineJoins`/
  `htLineExpr`, `optionsCombinedCTE`, `productsSortColumn`/
  `optionsSortColumn`) — pas de SQL retranscrit à la main, donc le plan
  mesuré est provablement la même requête que la production exécute. Exécuté
  avec les VRAIES constantes post-Phase-5 (`SET LOCAL work_mem = '8MB'`,
  `statement_timeout = 4000`, via `Repository.runTx`), contre le staging
  Render, sur l'établissement le plus lourd **réellement présent sur
  staging** (vérifié par requête, pas supposé depuis PROD) :
  établissement 212, 34 440 lignes `orderitems` — coïncidence avec le
  "merchant 212" cité comme le plus gros de PROD dans les commentaires du
  code, pas une hypothèse reprise sans vérifier. Fenêtre all-time
  (2000-01-01 → aujourd'hui), taille de page élargie à 200 (au-delà du
  défaut 50) pour forcer le pire cas.
  - **Options (`GetOptionsPage`)** : 1089 ms, aucun spill — chaque `Sort
    Method` est `quicksort`, chaque `HashAggregate`/`Hash` a `Batches: 1`,
    pic mémoire mesuré ~1,9 Mo (`Memory Usage`), très en dessous de 8 Mo.
  - **Produits (`GetProductsPage`)** : 1474 ms, aucun spill — même profil,
    pic mémoire ~3,3 Mo.
  - Aucune ligne `external merge` / `temp read` / `temp written` /
    `Disk:` dans les deux plans complets (`EXPLAIN (ANALYZE, BUFFERS)`).
  - **Conclusion : pas de débordement disque à 8 Mo**, sur les deux
    requêtes les plus lourdes du paquet, à la volumétrie réelle la plus
    haute présente sur staging. Aucun arbitrage à faire pour l'instant — si
    une requête future spillait à 8 Mo, ce serait un vrai arbitrage
    connexions/mémoire à trancher, pas un seuil à relever en silence (les
    deux constantes portent maintenant ce rappel dans leur commentaire).
- **Durées à 1, 2, 3 établissements** (même mesure temporaire, requêtes
  contre les 3 établissements staging les plus lourds — 212, 223, 2 —
  fenêtre 12 mois, mesurées au niveau requête/DB, pas bout-en-bout HTTP :
  `cmd/analytics_bench` existe déjà pour ça mais exige des jetons bearer par
  établissement indisponibles dans cet environnement ; le coût dominant est
  la requête DB, pas le HTTP par-dessus) :
  | Établissements | Options | Produits | CA (totaux) |
  |---|---|---|---|
  | 1 | 715 ms | 955 ms | 172 ms |
  | 2 | 601 ms | 1005 ms | 179 ms |
  | 3 | 695 ms | 927 ms | 198 ms |

  Options et Produits restent globalement plats (dominés par le volume de
  l'établissement le plus lourd du trio, pas par leur nombre — la variance
  observée est du bruit de mesure sur du staging partagé, pas une tendance).
  Le CA croît légèrement et régulièrement (172→198 ms), cohérent avec un
  `COUNT`/`SUM` dont le coût suit le volume de lignes scannées. Aucune des
  trois n'approche le fusible de 4000 ms.
- **`cmd/analytics_bench/main.go` corrigé en passant** : `explainSpill`
  utilisait un `work_mem` codé en dur à `16MB` — un vrai bug fonctionnel une
  fois Phase 5 déployée (l'outil aurait mesuré le spill sous le MAUVAIS
  `work_mem`, pas celui réellement en production). Lit maintenant
  `database.AnalyticsWorkMemMB`/`AnalyticsMaxOpenConns` directement, plus
  aucune figure recopiée à la main dans ce fichier.
- **Vérifié** : `go build ./...`, suite `postgres_integration` complète du
  paquet analytics contre staging (aucune régression — le paquet ne dépend
  pas de la valeur exacte des constantes, seulement `Repository.runTx` qui
  les applique déjà par construction), suite unitaire, aucun fichier de
  mesure temporaire laissé dans le dépôt.

### PROMPT 24 Phase 4 — Graphiques à N séries (wello-back-office, front seul) (2026-09-06)

- **`src/utils/merchantColors.ts`** : `merchantColor(merchantId)`, même
  contrat que `channels.ts`'s `channelColor` — fonction pure, dérivée de
  l'identifiant, jamais du rang dans le tableau affiché. Hash de chaîne
  (djb2-like) plutôt qu'un modulo sur l'entier brut : des `merchant_id`
  séquentiels ("212"/"213") tomberaient sinon trop souvent sur des couleurs
  adjacentes de la palette.
- **Palette délibérément disjointe de `CHANNEL_COLORS`** : violet/pink/
  indigo/fuchsia/rose/purple contre le bleu/vert/ambre/cyan/teal des
  canaux — deux échelles catégorielles distinctes, vérifié à l'œil (aucun
  chevauchement de teinte) plutôt que réutiliser/éclaircir la palette
  existante.
- **`EstablishmentComparisonChart.tsx`** (composant partagé, un seul
  endroit pour la logique couleur/légende) : un graphique à barres
  horizontales, une barre par établissement — pas un graphique multi-lignes,
  parce que `by_merchant` sur les 5 onglets comparables est un total par
  établissement sur la période, pas une série temporelle par établissement
  (contrairement au `by_channel_ttc_cents` journalier de CA/Commandes). Une
  ligne aurait supposé une donnée qui n'existe pas côté backend. Légende
  toujours rendue (`Legend payload={...}`, pas la légende par défaut de
  Recharts qui ne sait pas construire une entrée par `Cell` sur un seul
  `dataKey`).
- **Remplace les tableaux simples posés en Phase 3** sur les 5 onglets
  comparables (CA, Commandes, Règlements, TVA, Annulations) — même
  composant partagé partout, seuls la valeur et son formatage changent par
  onglet.
- **Vérifié, précisément sur le point que le prompt signale comme piège
  classique** : test interactif (mêmes conditions que la Phase 3 — session
  factice, hôte API réel bloqué) avec 5 établissements sélectionnés, lecture
  directe des couleurs SVG (légende + barres) avant/après désélection du
  PREMIER établissement de la liste. Résultat : chaque établissement restant
  garde exactement sa couleur (`COLOR STABILITY CHECK: PASS`, comparaison
  automatisée des couleurs par nom d'établissement avant/après) — la
  désélection ne décale aucune couleur, contrairement à ce qu'un
  index-dans-le-tableau-affiché aurait produit. Capture d'écran à 5 séries :
  cinq barres et cinq entrées de légende toutes visuellement distinctes,
  lisibles, sans chevauchement avec la palette canaux visible sur le même
  écran (graphique "Évolution CA" juste en dessous). Fixtures de mock
  repassées à leur état d'origine (1 établissement, `scope` non dynamique)
  après vérification.
- **`tsc --noEmit`, `vite build`, ESLint** : propres (mêmes warnings
  `exhaustive-deps` pré-existants qu'aux phases précédentes, aucune erreur).

### PROMPT 24 Phase 3 — Le sélecteur (wello-back-office, front seul) (2026-09-06)

- **Nouveau composant `EstablishmentFilter.tsx`**, global, à côté du filtre de
  période dans `DashboardAnalysis.tsx` — pas le pattern par-onglet de
  `ChannelFilter` (PROMPT 18) : la portée établissements est commune à toute
  la page, comme la période, donc l'état vit dans `DashboardAnalysis`, pas
  dans chaque onglet. Chips (`SelectableChip`, déjà utilisé pour tags/
  allergènes) + `ToggleGroup` (cumulé/comparé) pour le mode.
- **Rendu conditionnel** : la carte "Établissements" (et le rappel de portée
  dans chaque onglet) ne s'affiche que si `accessibleMerchants.length > 1` —
  un compte à un seul établissement n'a rien à filtrer ni à comparer, la
  carte serait un rappel d'une évidence sur chaque onglet.
- **Sélection par défaut** : l'établissement du token
  (`authData.session.merchant_id`, déjà utilisé par `Header.tsx`), avec repli
  sur le premier de la liste si absent — décision du prompt appliquée
  littéralement.
- **Bascule automatique en comparé** : passer de 1 à 2 établissements
  sélectionnés bascule `comparisonMode` sur `'compare'`
  (`handleMerchantSelectionChange` dans `DashboardAnalysis.tsx`) — la
  bascule ne se redéclenche qu'au FRANCHISSEMENT du seuil (1→2), pas à
  chaque changement, pour que revenir de 3 à 2 établissements ne réinitialise
  pas un choix "cumulé" déjà fait par l'utilisateur. Le sélecteur de mode
  est désactivé (`disabled`, jamais masqué) sous 2 établissements sélectionnés.
- **`ScopeSummary`/`AggregationNotice`** (`ScopeNotice.tsx`, partagé) :
  chaque onglet affiche les établissements réellement renvoyés par
  `scope.merchant_ids` (jamais ceux demandés), et les 5 onglets non
  comparables (Produits, Options, Clients, Remises, Vente additionnelle)
  affichent un bandeau explicite en mode comparé à 2+ établissements — "cet
  onglet agrège", jamais un mode qui semble actif sans effet visible.
- **`merchant_ids` maintenant envoyé partout**, pas seulement sur les 5
  onglets comparables : `analyticsService.ts` ne l'envoyait auparavant sur
  AUCUN appel réel (vérifié au prompt 23 — seul `scope` en lecture existait
  dans les types TS). Les 5 onglets non comparables et les 3 blocs
  nominatifs (`getClientsTop`, `getUpsellByStaff`,
  `getCancellationsByStaff`) le reçoivent aussi désormais — un bloc nominatif
  doit continuer à se masquer sur 403 quand le droit manque sur un seul des
  établissements sélectionnés (déjà géré côté serveur par
  `requireKeyOnAllMerchants`, PROMPT 23 Phase 2 ; ce lot fait juste en sorte
  que la sélection réelle de l'utilisateur atteigne enfin ces 3 endpoints).
- **Phase 4 non anticipée délibérément** : les 5 onglets comparables
  affichent un tableau simple "par établissement" (nom, total, compte) sous
  les graphiques existants quand `group_by=merchant`, pas encore les
  graphiques à N séries avec couleurs stables — c'est explicitement Phase 4
  ("les graphiques à N séries"), une phase séparée dans le prompt.
- **Vérifié en conditions quasi réelles** : `tsc --noEmit` et `vite build`
  propres ; ESLint ne remonte que des warnings `exhaustive-deps` de la même
  nature que ceux déjà présents ailleurs dans ce fichier (aucune erreur).
  Vérification interactive : serveur de dev Vite lancé avec
  `VITE_USE_MOCK=true`, session `localStorage.authData` factice
  (`access.admin=true`) pour passer `ProtectedRoute` sans backend réel, hôte
  API réel (`welloresto-api-staging.onrender.com`) bloqué via interception
  réseau Playwright pour éviter qu'un 401 sur un appel non mocké
  (`/me/permissions`, hors `withMock`) ne vide la session factice
  (`clearAuthAndRedirect`). Avec 2 établissements mockés temporairement :
  carte "Établissements" visible avec ses 2 chips, sélection du second
  bascule le mode sur "Comparé" (confirmé), désélection revenant à 1 désactive
  le bouton "Cumulé" (confirmé), le bandeau d'agrégation apparaît sur l'onglet
  Produits en mode comparé (confirmé, capture d'écran). Fixture mock repassée
  à 1 établissement après vérification — aucun changement de test laissé en
  place.

### PROMPT 24 Phase 2 — `group_by` sur Règlements, TVA, Annulations (2026-09-05)

- **Règlements** : `PaymentsRequest.GroupBy` + `PaymentsResponse.ByMerchant`
  ([]`PaymentsMerchantTotal`), même schéma que CA/Commandes —
  `Repository.GetPaymentsByMerchant` fait un simple `GROUP BY o.merchant_id`
  sur `paymentsScopeJoin` (même filtre `payments.enabled = TRUE`, mêmes
  jointures que `GetPaymentsTotals`). Aucun apportionnement requis (SUM/COUNT
  directs, pas de figure dérivée).
- **Annulations** : `GroupBy` ajouté à `CancellationsRequest`
  uniquement (l'endpoint d'agrégats) — le classement nominatif par serveur
  (`/cancellations/by-staff`) reste fusionné quel que soit le mode, décision
  explicite du prompt, non touché par ce lot. Deux nouvelles requêtes
  groupées par `merchant_id` (`GetOrdersCreatedCountByMerchant` — dénominateur
  du taux, `GetCancellationsTotalsByMerchant`), fusionnées côté service
  (`Service.cancellationsByMerchant`) puisqu'elles portent sur deux scopes SQL
  différents (tout ce qui a été créé vs. seulement ce qui a été annulé) —
  un établissement avec commandes mais 0 annulation doit apparaître avec
  `CancelledCount: 0`, pas disparaître d'une jointure interne.
- **TVA — le point d'attention du prompt.** `apportionVATByRate`/
  `apportionVATByChannel` (le "plus fort reste" du PROMPT 06 §1) tournent
  maintenant **une fois par établissement** en mode comparé
  (`Service.buildVATByMerchant`), chacune contre le `TotalHTCents` **propre à
  cet établissement** (`Repository.GetVATTotalsByMerchant`, une ROUND()
  indépendante par établissement) — jamais contre le total combiné du
  périmètre. Trois nouvelles requêtes groupées par `merchant_id` en plus de
  `rate`/`channel` (`GetVATTotalsByMerchant`, `GetVATByRateByMerchant`,
  `GetVATByChannelByMerchant`), même UNION ALL produit×frais de livraison que
  les versions non groupées.
- **Vérifié, précisément sur ce point** :
  `TestGetVAT_GroupByMerchant_PartsSumToOwnTotal_Postgres` construit 2
  établissements à des totaux HT délibérément différents (1000 exact vs. un
  mélange 20%/10% avec reste réel) et vérifie que le `by_rate`/`by_channel`
  de **chacun** somme exactement à **son propre** `TotalHTCents`/
  `TotalVATCents` — pas au total combiné. `apportionCents` force sa sortie à
  sommer exactement à la valeur qu'on lui passe : ce test aurait donc échoué
  immédiatement si l'implémentation avait apportionné contre le total combiné
  au lieu du total par établissement (l'établissement à une seule part se
  serait vu attribuer 100% du total combiné au lieu de son propre total).
- **Limite connue, non cachée** : `VATMerchantTotal.TotalHTCents` est
  arrondi indépendamment par établissement (une `ROUND()` par groupe), alors
  que le total combiné du scope est arrondi une seule fois sur la somme brute
  — un écart de ±1 centime entre `Σ(TotalHTCents par établissement)` et le
  total cumulé non groupé est mathématiquement possible (le problème classique
  `round(a)+round(b) ≠ round(a+b)`), même si le jeu de test actuel ne
  l'exhibe pas. `TotalTTCCents`, lui, reste une somme entière exacte (jamais
  dérivée), donc la réconciliation TTC "somme des séries comparées = total
  cumulé" tient toujours au centime près — c'est HT/VAT spécifiquement, déjà
  une figure dérivée avant ce lot, qui hérite de cette limite d'arrondi
  indépendant. Pas un choix caché : signalé ici pour arbitrage si un client
  fait un jour ce rapprochement au centime.
- Reconciliation en plus pour les 4 autres tabs comparables :
  `TestGetPaymentsByMerchant_Postgres`/`TestGetCancellationsByMerchant_Postgres`
  (parts = total non groupé, exact), plus les tests bout-en-bout déjà
  existants pour CA/Commandes (`TestGetOrders_GroupByMerchant_EndToEnd`)
  étendus au même schéma pour Règlements et Annulations
  (`TestGetPayments_GroupByMerchant_EndToEnd`,
  `TestGetCancellations_GroupByMerchant_EndToEnd`), y compris le rejet d'un
  `group_by` invalide (`ErrInvalidRequest`).
- **Vérifié** : `go build ./...`, suite unitaire du package, suite complète
  `postgres_integration` du package contre le staging Render (58 tests, tous
  verts).

### PROMPT 24 Phase 1 — Endpoint des établissements accessibles (2026-09-05)

- **Pas de doublon de `ResolveAccessibleMerchants`.** Aucun endpoint
  n'exposait déjà la liste nommée avant ce lot — vérifié dans `handler.go` et
  `cmd/api/routes.go` (aucune route "merchants"/"establishments"). Le seul
  candidat proche, `auth.AuthRepository.GetMerchants` (alimente le sélecteur
  d'établissement au login), a une portée différente et plus large : aucun
  filtre `pos.analytics`, aucun filtre `enabled`/`login_enabled`. Le réutiliser
  aurait fuité des établissements hors du périmètre analytics — écarté.
- **Nouveau : `GET /analytics/merchants`**, gardé par `permission.POSAnalytics`
  (pas `reports.sales.read` comme le reste du groupe `/analytics` — cet
  endpoint EST la donnée du gate pos.analytics, pas un chiffre de vente).
  `Service.GetAccessibleMerchants` appelle `ResolveAccessibleMerchants` (la
  même fonction que chaque onglet, aucune seconde implémentation de la règle
  de portée) puis `Repository.GetMerchantNames` pour les libellés. Une scope
  vide renvoie `{"merchants": []}`, jamais une erreur.
- `Repository.GetMerchantNames` doit caster `merchant.id` (entier) en texte
  pour le comparer au tableau `merchantIDs` (`CAST(id AS TEXT) = ANY(?)`) —
  même contrainte que `auth.authMerchantJoinCast()`, ce package étant déjà
  Postgres-only (le `= ANY(?)` de `scope.go` le suppose partout).
- **Vérifié** : `go build ./...`, `go vet ./internal/modules/analytics/...`,
  suite unitaire du package, suite `postgres_integration` complète du package
  contre le staging Render (`RENDER_STAGING_DATABASE_URL`) — nouveau test
  `TestGetAccessibleMerchants_Postgres` inclus (établissement avec
  `pos.analytics` → visible et nommé ; établissement avec
  `reports.sales.read` seul mais pas `pos.analytics` → exclu ; établissement
  sans lien du tout → exclu ; utilisateur sans aucun accès → liste vide, pas
  d'erreur). `go test ./internal/...` fait apparaître 3 échecs pré-existants
  et sans rapport (`planning/leave`, `planning/swaps`, `ubereats`) — non
  causés par ce lot.

### PROMPT 23 lot 2 — Contrat, cache, performance (Phase 3+4+5) (2026-09-05)

- **Phase 3 — le paramètre de requête `merchant_ids` était déjà sur les 10
  endpoints** avant ce lot (`ResolveAccessibleMerchants`/
  `ValidateRequestedMerchants` déjà branchés partout, 403 strict déjà en
  place) — rien à ajouter ici, seulement à re-vérifier après l'élargissement
  de portée de la Phase 1 (fait : les tests postgres_integration du lot 1
  couvrent déjà cette validation contre le nouveau périmètre).

- **`group_by` : état réel, pas supposé.** Vérifié endpoint par endpoint —
  seuls **2 des 10** endpoints portent ce champ au contrat
  (`RevenueRequest`/`OrdersRequest`, hérité de PROMPT 03) ; les 8 autres
  (Règlements, TVA, Annulations, Produits, Options, Clients, Vente
  additionnelle, Remises) n'en ont jamais eu. **Sur les 2 qui l'ont, un seul
  fonctionnait réellement** : `GetOrders` acceptait `group_by=merchant`,
  l'échait dans `Scope.GroupBy`, l'incluait même dans la clé de cache — sans
  jamais rien calculer (`OrdersResponse` n'avait pas de champ `ByMerchant` du
  tout). Un paramètre mort, pire qu'absent : un appelant ne pouvait pas
  distinguer "ignoré" de "rien à ventiler".

  **Corrigé** : `Repository.GetOrdersByMerchant` (nouveau, même gabarit que
  `GetRevenueByMerchant` — `GROUP BY o.merchant_id` sur des `COUNT`/`SUM`
  entiers, donc les lignes somment exactement au total ungroupé, aucune
  apportion nécessaire, contrairement à un HT dérivé) + `OrdersResponse.
  ByMerchant` + validation stricte de `group_by` dans `GetOrders` (valeur
  invalide → 400, comme `GetRevenue`). **Aucun test n'existait pour
  `GetRevenueByMerchant` ni pour cette validation avant ce lot** — c'est
  précisément comme ce défaut d'`GetOrders` est passé inaperçu si longtemps ;
  3 tests postgres_integration nouveaux couvrent maintenant les deux
  endpoints, avec la vérification de réconciliation exacte
  (`sum(by_merchant) == total ungroupé`).

  **Décision de périmètre, assumée plutôt que silencieuse** : je n'ai PAS
  étendu `group_by` aux 8 autres endpoints dans ce lot. Le brief demande de
  "vérifier" l'implémentation, pas de la généraliser ; Règlements/TVA/
  Annulations partagent la forme "une série de KPI période" de CA/Commandes
  et s'y prêteraient (TVA demanderait de réutiliser `apportionCents` pour
  HT/TVA par établissement, exactement comme `by_rate`/`by_channel` le font
  déjà — travail cadré mais non trivial) ; Produits/Options/Clients/Vente
  additionnelle/Remises sont des tables paginées ou des classements, pas des
  séries — "comparé vs cumulé" n'y a pas le même sens évident et
  demanderait une décision produit propre (pagination par établissement ?
  tableaux séparés ?) hors du périmètre "backend seul, socle" de ce lot.
  Recommandation posée mais non implémentée : étendre en priorité à
  Règlements/TVA/Annulations si l'usage réel le demande.

- **Phase 4 — le cache était déjà correct.** Les 5 fonctions de clé
  (`buildCacheKey`, `buildProductsCacheKey`, `buildOptionsCacheKey`,
  `buildClientsCacheKey`, `buildDiscountsCacheKey`) triaient déjà
  `merchantIDs` avant hachage — aucune n'avait de test le prouvant. 5 tests
  unitaires ajoutés (`cache_test.go`), un par fonction : `[212,228]` et
  `[228,212]` produisent la même clé, `[212]` et `[212,228]` des clés
  différentes.

- **Phase 5 — mesure directe des requêtes (pas HTTP), via le pool analytics
  réel** (`database.NewAnalyticsPostgres`, mêmes réglages fusible que la
  prod : `statement_timeout=4000ms`, `work_mem=16MB` en `SET LOCAL`),
  contre staging, protocole 5 exécutions/1re jetée/médiane, répété deux fois.
  Bundle mesuré = exactement les 8 requêtes que `GetRevenue` exécute pour
  CA/12 mois/`include_ht=true` (3 périodes × TTC+HT, + timeline + by_channel)
  — une première version du script ne mesurait qu'une période et
  sous-comptait le coût réel d'un facteur ~3, corrigée avant de conclure.

  | Établissements | Médiane bundle (ms) | Requête la plus lente (ms) |
  |--:|--:|--:|
  | 1 (212 seul) | ~2 500–3 200 | ~800–900 |
  | 5 (top 5 volume, dont 212) | ~4 300–4 500 | ~1 400 |
  | 10 (top 10 volume, dont 212) | ~6 000–6 400 (pic isolé 8 200) | ~2 200 |
  | 20 (30 établissements staging, 20 pris, dont 212 + 19 quasi vides) | ~5 800–6 300 | ~2 700 |

  **Aucune exécution n'a déclenché le `statement_timeout`**, mais la requête
  la plus lente est passée de 800ms (1 étab.) à 2 700ms (20 étab.) — 68% du
  budget de 4000ms sur une seule requête, en ne comptant qu'**un seul**
  établissement réellement volumineux (212, ~9 600 commandes/an) parmi les
  20 : staging n'a qu'un seul établissement à ce volume, donc ce chiffre est
  un **plancher**, pas un plafond — un groupe réel avec plusieurs
  établissements aussi actifs que 212 sélectionnés ensemble dépasserait
  vraisemblablement le fusible avant 20. La latence bout-en-bout (4,3–6,4s
  dès 5 établissements) est de toute façon déjà dégradée pour un affichage
  interactif, indépendamment du fusible.

  **Plafond recommandé : 5 établissements** pour la requête la plus lourde
  (CA, 12 mois, `include_ht=true`) — marge confortable sur le fusible
  (~1,4s max par requête) tout en restant utile pour un comparatif réel.
  **Non appliqué en code dans ce lot** (le brief demande une recommandation,
  pas une garde technique — décision produit/frontend, hors périmètre
  "backend seul" de PROMPT 23). Si l'usage réel pousse au-delà de 5-10
  établissements réguliers, c'est un argument direct pour la
  pré-aggrégation déjà anticipée par `logInstrumentation`
  (`analytics_query`) plutôt que pour desserrer le fusible ou le pool à 2
  connexions.

- **Vérifié** : `go build ./...` propre, suites unitaires (`analytics`,
  `auth`, `permission`, `cmd/api`) et `postgres_integration` du paquet
  `analytics` (32s, tout vert) contre staging. Script de mesure Phase 5
  jetable (staging uniquement, jamais commité) — supprimé après usage.

### PROMPT 23 lot 1 — Socle multi-établissements : périmètre + vérification croisée (Phase 1+2) (2026-09-05)

- **Étape intermédiaire, validée avec l'utilisateur avant de poursuivre** :
  Phases 1 et 2 seules (résolution du périmètre + `Has(clé, merchant_id)`),
  Phases 3-5 (contrat sur les 10 endpoints, cache, mesures perf/plafond)
  restent à faire dans un lot suivant.

- **`ResolveAccessibleMerchants` change de nature, pas seulement de contenu** :
  passe d'une fonction pure (`internal/modules/analytics/scope.go`,
  `[]string{user.MerchantID}`, gardée par un commentaire interdisant
  explicitement d'en faire une requête) à une méthode de `Repository`
  (`repository.go`) — la portée réelle est maintenant "tout établissement où
  `users_rights` (actif : `enabled AND login_enabled`) donne `pos.analytics`
  à cet utilisateur", en **une seule requête** (`LEFT JOIN role_permissions`,
  pas de boucle par lien). L'ancien garde-fou est explicitement caduc : la
  décision produit qu'il anticipait ("multi-établissement = mécanisme
  séparé, pas encore décidé") est maintenant prise par PROMPT 23 lui-même.

- **Piège user_id vide neutralisé structurellement, pas par convention** :
  `AND ur.user_id <> ''` dans le WHERE rend impossible de fusionner les 4
  lignes `user_id=''` de staging avec le périmètre de quiconque, y compris
  dans le cas dégénéré où `userID` sondé vaudrait lui-même `""` — testé
  explicitement (`TestResolveAccessibleMerchants_Postgres`, avec 4
  établissements orphelins créés pour l'occasion) plutôt que supposé sûr par
  construction de la requête appelante.

- **Les deux mondes RBAC, dans la même requête** : `role_id` renseigné →
  jointure vers `role_permissions` (aucun court-circuit `system_key=admin`,
  cohérent avec le retrait de ce court-circuit dans `Has()` au lot 11) ;
  `role_id` NULL → repli sur `users_rights.admin` seul, puisque
  `pos.analytics` n'a **aucune** entrée dans `legacyPermissionFallback`.
  Testé séparément (`TestResolveAccessibleMerchants_BothRBACWorlds`) contre
  staging.

- **Établissement du token absent du résultat : décision assumée, pas un
  bug** — si le lien courant ne porte pas `pos.analytics`, l'utilisateur ne
  verrait de toute façon pas l'onglet Analyse côté front (même droit qui
  gate le menu). `ValidateRequestedMerchants` (inchangé) rejette alors tout
  `merchant_id` demandé avec un 403, jamais un filtrage silencieux.

- **`Has(clé, merchant_id)` construit sans toucher au repli historique** :
  `Repository.HasForMerchant` (nouvelle méthode) réplique `Has()` pour un
  établissement quelconque — une seule requête (role_id + jointure
  `role_permissions` en une fois), `Rights.Admin` sinon
  `auth.LegacyFallback(clé, rights)`, un nouvel accesseur exporté (purement
  additif) sur `legacyPermissionFallback` : réutilise la même map plutôt que
  d'en dupliquer le contenu, pour qu'il n'y ait rien à garder synchronisé à
  la main. `internal/modules/auth/permissions.go` n'est autrement pas
  modifié.

- **Test de non-divergence, exigé par le brief, vérifié contre staging** :
  `TestHasForMerchant_AgreesWithHasOnTokenMerchant` construit un
  `UserLoginRow` à la main et compare `user.Has(clé)` à
  `repo.HasForMerchant(...)` sur le **même** établissement, dans les deux
  mondes RBAC (rôle porteur/non-porteur, `admin=true`, `admin=false` avec et
  sans entrée de repli) — les deux implémentations concordent aujourd'hui.

- **Appliqué aux 3 blocs nominatifs** (Annulations/staff, Vente
  additionnelle/staff, Top clients) : `Service.requireKeyOnAllMerchants`
  vérifie `reports.staff_performance.read` (ou `customers.manage` pour Top
  clients) sur **chaque** `merchant_id` de la portée validée, avant de
  construire la réponse — sinon `ErrNominativeAccessDenied` → 403
  (`nominative_access_denied`, distinct de `merchant_not_accessible`).
  Aucun classement partiellement amputé.

- **Vérifié contre staging (lecture seule)** : les 4 nouveaux tests
  (`scope_postgres_integration_test.go`) passent, ainsi que la suite
  `postgres_integration` complète du paquet `analytics` (34,5 s, aucune
  régression). `go build ./...` et les suites unitaires (`analytics`,
  `auth`, `permission`, `middleware`, `cmd/api`) sont vertes. Deux paquets
  non touchés par ce lot (`roles`, `planning/employees`) ont des tests déjà
  en échec sur cette copie de travail avant toute modification de ce
  lot — confirmé en relançant ces mêmes suites sans aucun changement
  d'environnement (`go test ./internal/modules/roles/...` seul reste vert ;
  l'échec n'apparaît qu'en combinant `-tags postgres_integration` avec
  `DB_DIALECT=postgres` exporté pour TOUT le paquet, y compris ses tests
  `sqlmock`, qui supposent `?` non traduit — artefact de la commande de test
  choisie pour vérifier ce lot, pas une régression qu'il introduit).
  `TestSystemAdminRolesContainFullCatalog_Postgres` échoue par ailleurs pour
  une raison réelle mais préexistante et documentée (DROITS.md §5.2) :
  `cmd/seed_system_roles` n'a jamais été relancé depuis l'ajout de
  `reports.staff_performance.read` (PROMPT 10) — sans effet sur ce lot
  (aucun rôle admin ne perd `pos.analytics`, déjà backfillé pour lot 10).

### PROMPT 22 — Onglet Remises (2026-09-05)

- **Contexte** : la maquette de cet onglet décrivait un produit qui n'existe
  pas (`discount_type`, section remises panier) — corrigé plutôt que reproduit,
  sur la base du modèle figé par PROMPT 21 (`discount_redemptions`). Un seul
  endpoint (`POST /analytics/discounts`, `reports.sales.read`) : aucune donnée
  nominative dans cet onglet.

- **Groupement par `discount_id` (integer, `discounts.discount_id_new`),
  jamais par libellé** : `internal/modules/analytics/discounts.go` grouoe
  `GetDiscountsPage` sur `dr.discount_id` seul — un renommage ne fragmente ni
  ne fusionne jamais l'historique d'une remise. `discounts` est LEFT JOIN
  (jamais INNER) pour le libellé : une remise supprimée (`enabled=false`)
  garde ses lignes historiques visibles, avec `is_deleted=true`.

- **Aucune section remises panier** : `applyCartDiscount` n'existe nulle part
  (confirmé par PROMPT 21) — pas une section vide (qui laisserait croire à
  zéro), rien du tout.

- **Dénominateur du taux de remise moyen, tranché explicitement** :
  `total_discounted_cents / reference_revenue_ttc_cents`, où le second est le
  CA TTC de **toute** la période (commandes remisées ou non), filtré par
  canal — pas le CA des seules commandes remisées (l'autre choix également
  défendable). Motif : répond à "quelle part de ce qui a été encaissé a été
  remisée", directement comparable à la tuile CA des autres onglets. Le champ
  `reference_revenue_ttc_cents` est toujours renvoyé à côté, jamais un taux nu.

- **Seuil de matérialité réutilisé verbatim** : `discountsMinOrdersForRate =
  staffCancellationMinOrders` (30, cancellations.go) gate `DiscountRatePercent`
  ET `OrdersWithDiscountRatePercent` sur `DiscountedOrdersCount` — en dessous,
  les deux sont absents (jamais 0), mais les volumes bruts et le volume de
  référence restent toujours affichés (`discounted_orders_count`,
  `total_orders_count`, `reference_revenue_ttc_cents`).

- **Impact sur la marge, au contrat, à `null` en pratique** : `DiscountsMarginCoverage`
  reprend le contrat `ProductsCostCoverage` à l'identique (même seuil de
  couverture 20%, `coversCoverageThreshold`), restreint aux redemptions
  `scope=PRODUCT_LINE` jointes à `orderitems.cost_price_unit` — une remise
  `CART` ne peut être rattachée à aucune ligne costée, donc exclue du
  numérateur ET du dénominateur (jamais un faux zéro). `cost_price_unit` étant
  `null` sur la quasi-totalité des lignes (PROMPT 07 lot 1), l'écran affiche
  "non disponible", jamais un montant.

- **Plancher vs mesure complète, porté par `is_reconstructed` dans le
  contrat lui-même, pas en note de page** : chaque période distingue
  `reconstructed_amount_cents`/`measured_amount_cents` (et leurs comptes de
  redemptions). `MeasurementCompleteFrom` (nouveau, `GetDiscountsMeasurementCompleteFrom`)
  expose la date de bascule réelle — `MIN(created_at)` parmi les lignes
  `is_reconstructed=false`, **non bornée à la période demandée** (fait global
  sur l'établissement, pas une fenêtre de rapport) — `nil` tant qu'aucune
  écriture en direct n'existe. Le frontend affiche un bandeau "Montant
  plancher, pas un total" tant qu'une part de la période est reconstituée,
  avec la date de bascule ou l'absence totale de mesure directe selon le cas
  — jamais une mention en pied de page.

- **Vérification en lecture seule, staging, merchant 212, 12 mois
  (2025-09-05 → 2026-09-05, transaction READ ONLY)** : montant total remisé
  110 840 centimes, intégralement reconstitué (367 redemptions, 0 mesuré en
  direct) — la répartition par remise (une seule remise réelle sur ce
  périmètre, "2 pizzas pour 12€") somme exactement au total. 186 commandes
  remisées sur 9 619 (1,93 %), CA de référence 16 016 930 centimes, taux de
  remise moyen 0,69 %. Marge : couverture 0 % (`cost_price_unit` jamais
  renseigné sur ces lignes), donc "non disponible" comme attendu. **Fait
  notable, vérifié à l'échelle globale (tous établissements)** : les 545
  lignes de `discount_redemptions` sont *toutes* `is_reconstructed=true` —
  aucune écriture en direct n'a encore eu lieu nulle part
  (`upsertOrderItemDiscountRedemption`, câblé par PROMPT 21, n'a pas encore
  tourné une seule fois en conditions réelles) : `MeasurementCompleteFrom` est
  `nil` partout aujourd'hui, pas seulement sur le merchant 212. Test
  d'accuracy dédié (`discounts_postgres_integration_test.go`, jeu de données
  synthétique) rejoué avec succès contre staging : somme exacte, répartition
  qui somme au total, `COUNT(DISTINCT order_id)` correct, partition par canal
  qui somme au total, et une remise supprimée d'une commande simulée
  "rouverte" (ligne `discount_redemptions` supprimée manuellement) disparaît
  bien immédiatement de tous les agrégats — cet onglet n'a aucune logique
  d'exclusion propre, il lit `discount_redemptions` tel quel.

- **Frontend** (`wello-back-office`) : `DiscountsAnalyticsTab.tsx` remplace le
  mock `renderDiscountsTab`/`getDiscountsAnalytics` synchrone dans
  `DashboardAnalysis.tsx` (filtre type de remise/`MultiFilter` retiré avec
  lui — n'existe plus). Réutilise `ChannelFilter` (prompt 18) et le gabarit
  pagination/tri serveur de Produits/Options. `analyticsService.ts` porte les
  nouveaux types `Discounts*`/`DiscountRow` et `getDiscountsAnalytics`/
  `exportDiscountsCSV` (reconstruit côté client à partir des données déjà
  chargées, même limite que Annulations/Clients — aucun endpoint CSV backend
  n'existe). Vérifié en navigateur réel (Playwright headless, mode mock local
  `--mode smoketest`, session injectée via `localStorage.authData` pour
  contourner l'auth réelle indisponible en local) : rendu correct du bandeau
  plancher, des 4 tuiles, du tableau trié/paginé et de l'interaction de tri —
  aucune erreur console hors l'échec CORS déjà pré-existant de la
  revalidation `/me/permissions` en environnement local (non lié à ce lot).

- **Hors périmètre, comme demandé** : le modèle de remises (figé par PROMPT
  21) n'a pas été touché ; aucune fonctionnalité de remise panier construite.

### PROMPT 21 — Assainir le modèle de remises, Phases 1-3+5 (2026-09-05)

- **Contexte** : `discounts.discount_id` (varchar) et `orderitems.discount_id`
  (integer) ne peuvent structurellement pas se référencer par jointure directe
  depuis toujours (rapport 57) ; le brief demandait un plan validé avant tout
  code, puis expansion/déploiement/contraction, aucune suppression de colonne
  jouée. Recon + plan complet présentés et validés en chat avant implémentation
  (pas de rapport séparé dans `docs/` — recon + plan tiennent dans cette
  entrée, conformément au format `decisions.md`).

- **La prémisse « aucun front ne manipule discount_id » était fausse, corrigée
  avant d'agir** : vérifié dans le code que `wello-back-office` (CRUD promotions,
  `GET/PATCH/DELETE /menu/discounts/{discount_id}`) et le POS `wello_resto_flutter`
  (`product_payload.dart` renvoie `discount_id` au serveur) font transiter cette
  valeur en lecture-écriture — jamais générée ni interprétée côté client
  (`discounts/service.go:60` écrase toujours l'ID côté serveur), donc sans
  rupture fonctionnelle, mais la décision de garder le **type JSON** `discount_id`
  en `string` (désormais le texte décimal d'un entier, plus un
  `discount-<uuid>`) plutôt que de basculer en `number` a permis d'éviter toute
  coordination de déploiement avec ces deux fronts — aucun n'a été touché.

- **Chiffrage réel (staging, `RENDER_STAGING_DATABASE_URL`)**, tous les
  chiffres cités par le brief confirmés à l'identique : 19 remises (9 au
  format numérique legacy actif, 10 `discount-<uuid>` toutes "Happy Hour"
  merchant 2, **0 ligne orderitems** pour ces 10 — jamais utilisables via la
  colonne integer) ; 4 609 `orderitems.discount_id` non nuls dont **17
  orphelines** (5 valeurs : 81/83/84/89/90, remises supprimées) ; **545**
  lignes `base_price ≠ price`, **100% avec un `discount_id` qui matche encore
  une remise réelle** (aucun recoupement avec les 17 orphelines) ;
  `orders.cart_discount_amount > 0` : **0 ligne**, confirmé jamais alimentée.
  `applyCartDiscount` (citée par un commentaire de `create_order_models.go`)
  **n'existe nulle part** — pas un bug de synchronisation, une fonctionnalité
  jamais construite.

- **Phase 2 (clé entière)** : `discounts.discount_id_new integer` attribué
  sans réécrire `orderitems.discount_id` — les 9 remises legacy gardent leur
  valeur numérique actuelle telle quelle (4 592 lignes orderitems valides
  inchangées bit à bit, vérifié), les 10 `discount-<uuid>` reçoivent des
  valeurs neuves après le plus grand entier déjà en circulation (calculé
  dynamiquement en SQL — la migration tourne aussi en prod, dont les valeurs
  réelles diffèrent). `discounts.legacy_discount_id` conserve l'ancien varchar,
  jamais supprimée. Élargi au passage à `discounts_products`/`discounts_schedules`/
  `discounts_products_options` (pas demandées nommément par le brief, mais
  même situation structurelle — cette dernière a une colonne `varchar(20)`,
  trop courte pour un futur `discount-<uuid>` de 45 caractères) : les trois
  reçoivent enfin de vraies contraintes `FOREIGN KEY` (aucune des trois n'en
  avait jamais eu, le mismatch de type l'en empêchait). Pas de FK sur
  `orderitems.discount_id` : les 17 orphelines la feraient échouer, laissées
  telles quelles comme acté. Bascule de la `PRIMARY KEY` elle-même différée à
  un lot de contraction futur. Migrations
  [`118_discounts_integer_key_expansion`](../migrations/todo/118_discounts_integer_key_expansion.up.sql)
  appliquées et vérifiées sur staging (idempotence testée par double
  application de la partie backfill).

- **Phase 3 (table de liaison)** : `discount_redemptions` (créée à vide par
  `041_cart_discounts`, jamais câblée — rapport 57) étendue plutôt que
  remplacée — `scope` (`PRODUCT_LINE`/`CART`, distinct de
  `discounts.discount_scope` qui décrit la configuration, pas une utilisation
  précise), `order_item_id` nullable, `customer_id` retypé `varchar→integer`
  (corrige le deuxième mismatch noté rapport 57), `is_reconstructed`.
  L'ancienne `UNIQUE(discount_id, order_id)` était fausse pour une remise
  ligne (la même remise peut s'appliquer à 2 lignes de la même commande) :
  remplacée par deux index uniques partiels, un par portée. Montant figé à
  l'application (pas un pourcentage recalculable) — même raisonnement déjà
  posé pour le coût de revient (PROMPT 07) : un prix qui change plus tard ne
  doit pas faire bouger un montant déjà accordé. Écriture en direct câblée
  dans `order_life_cycle.upsertOrderItemDiscountRedemption` (appelée par
  `InsertOrderItem` et par la boucle de `UpdateOrder`) : supprime la ligne de
  liaison si la remise est retirée d'une commande encore ouverte (cas cité par
  le brief comme le plus facile à manquer), upsert sinon — y compris pour
  "graduer" `is_reconstructed=false` si une commande fermée est rouverte puis
  réellement réécrite.

- **Phase 4 (cart_discount_amount) différée, pas juste laissée de côté** :
  vérifié qu'aucun code n'applique jamais une remise panier (`applyCartDiscount`
  n'existe pas) — maintenir la colonne "juste après chaque application"
  suppose une application qui n'existe pas. Décidé avec l'utilisateur :
  ce lot prépare seulement le modèle (`discount_redemptions` scope=CART prêt),
  le calcul/validation d'un code promo panier est un chantier produit à part,
  hors périmètre de cet assainissement.

- **Phase 5 (reprise de l'historique)** : les 545 lignes `base_price ≠ price`
  reconstituées dans `discount_redemptions` (`amount_applied_cents = base_price
  - price`, `scope='PRODUCT_LINE'` sans ambiguïté possible — `ORDER_TOTAL`
  n'existe que depuis Sprint 2 et `cart_discount_amount` n'a jamais été
  alimenté), marquées `is_reconstructed=true`. `customer_id` laissé `NULL` :
  `orderitems` ne porte pas cette information, une jointure via
  `orders.customer_id` serait une déduction de plus n'apportant rien à
  l'objectif. Migration
  [`119_discount_redemptions_historical_backfill`](../migrations/todo/119_discount_redemptions_historical_backfill.up.sql),
  idempotente (`ON CONFLICT DO NOTHING`, testé par double application),
  appliquée sur staging : 545/545 lignes, montant total vérifié égal à la
  somme réelle `base_price - price` du jeu de données source.

- **Ce qui reste à faire** (hors périmètre explicite de ce lot) :
  `cart_discount_id`/`cart_discount_code` — migration de suppression préparée
  ([`120_drop_cart_discount_legacy_columns`](../migrations/todo/120_drop_cart_discount_legacy_columns.up.sql)),
  **non jouée**. La simplification des jointures `orders/orders_fetcher_builder.go`
  et `orders/repository.go` (`CAST(oi.discount_id AS TEXT)` devenu inutile, un
  join direct `int = int` suffit désormais) n'a pas été faite dans ce lot —
  fonctionnellement toujours correcte telle quelle, cleanup sûr et non urgent
  à faire plus tard. `docs/migration-postgres/04-schema-postgres-target.sql`
  pas mis à jour dans ce lot (même pattern que les migrations 108-117 :
  reconciliation périodique en bloc, pas systématique migration par migration
  — voir rapport 63) : à faire lors du prochain passage de reconciliation.
  Bascule de la `PRIMARY KEY` de `discounts` vers `discount_id_new` : lot de
  contraction futur, une fois `legacy_discount_id`/`discount_id` confirmés
  plus lus nulle part.

### PROMPT 20 — Diagnostic vente additionnelle, Phase 1 (2026-09-05)

- **Contexte** : le prompt partait de trois hypothèses (moteur qui ne
  produit rien, suggestions non affichées, `is_upsell` non écrit) et
  demandait un diagnostic chiffré avant tout correctif. Rapport complet :
  [docs/audits/2026-09-05-upsell-diagnostic-prompt20.md](audits/2026-09-05-upsell-diagnostic-prompt20.md).

- **Cause dominante du faible volume, vérifiée en base (staging)** :
  `enable_upsell` n'est activé que sur **2 établissements sur 30**
  (`merchant_parameters`). Des établissements avec un volume de commandes
  bien supérieur à celui des 2 établissements actifs (617, 348, 189
  commandes clôturées/90j) ont **zéro** ligne `upsell_suggestions` —
  `generateUpsellSafe` retourne avant même de tenter une persistance
  quand le flag est faux. Constat produit (rollout pilote non généralisé
  ou oubli d'activation ?), pas un bug.

- **Même activé, le moteur "intelligent" ne se déclenche presque jamais** :
  312 des 313 lignes générées sont `source=featured_fallback` (dernier
  recours statique), 1 seule est `pattern`, aucune n'est `llm`. Le seuil
  de co-occurrence (`upsellMinCoOccur=5`/90j) semble structurellement dur
  à atteindre pour un volume de restaurant indépendant, y compris à 2000+
  commandes/trimestre — remonté comme arbitrage produit, seuil non
  modifié de ma propre initiative (consigne explicite du prompt).

- **`accepted_items` fonctionne mais n'a été exercé qu'une fois** dans
  toute l'histoire de la base (2026-07-02, le jour même de l'audit qui
  avait trouvé ScanNOrder non câblé) — mesure différente de
  `orderitems.is_upsell` (corrélation passive produit-dans-commande vs
  preuve d'interaction UI), les deux ne se corroborent pas mutuellement.

- **Surprise principale** : deux des trois correctifs attendus en Phase 2
  étaient déjà faits, sans trace documentée. `wello-resto-scannorder`
  envoie déjà `is_upsell` (`payload.ts:55`, `UpsellPopup.tsx:133`) dans le
  HEAD committé actuel — vérifié par lecture directe + `git show HEAD`,
  contredisant la prémisse du prompt ("aucune occurrence dans le dépôt").
  Aucun code touché pour ce point. Seul correctif appliqué : commentaire
  périmé de `stats/service.go:106` ("jusqu'à ce que l'app Flutter écrive
  is_upsell") corrigé pour pointer vers ce diagnostic plutôt que
  réaffirmer une cause qui ne tient plus depuis juin.

- **Traçabilité du build client (1.3)** : aucun mécanisme serveur ne
  persiste la version applicative réellement en usage par établissement.
  `app_version`/`app_version_merchant` ne servent qu'au contrôle de mise à
  jour (jamais d'écriture du côté serveur) et semblent à l'abandon (dernière
  version connue : avril 2025, alors que le client envoie déjà la version
  100 aujourd'hui). Piste la plus légère identifiée : `api_request_logs`
  capture déjà le corps de `/app/version/check` (`payload` jsonb), mais
  `merchant_id` y est vide sur cette route — recommandation remontée, pas
  implémentée (changement de logging sur route d'auth partagée, à valider).

- **Aucune donnée de production directe** : diagnostic mené contre
  `RENDER_STAGING_DATABASE_URL` (pas d'accès prod local,
  [[reference_staging_db_access]]). Les chiffres collent presque
  exactement à ceux cités par le prompt pour la production (287/26/1 vs
  284/258+26/1 avec dérive expliquée par le temps écoulé) — à confirmer
  par le lecteur avant de traiter ces chiffres comme définitifs pour la
  vraie prod.

### PROMPT 18 — onglet Clients (2026-09-05)

- **Contexte** : huitième onglet analytics branché en SQL direct. Deux
  routes, deux permissions, même découpage que l'onglet Annulations
  (PROMPT 10) : agrégats (nouveaux clients, taux de récurrence, segments,
  fréquence, panier par segment — `permission.ReportsSalesRead`, même porte
  que les 7 autres onglets) et classement nominatif Top Clients (nom, valeur
  vie, dernière visite, panier moyen — `permission.CustomersManage`, déjà
  existante, `is_sensitive`, aucune clé créée). Backend :
  `internal/modules/analytics/{clients,channels,models,service,handler}.go`,
  routes `POST /analytics/clients` et `POST /analytics/clients/top`
  (`cmd/api/routes.go`). Frontend : `ClientsAnalyticsTab.tsx`
  (wello-back-office), remplace `renderClientsTab`/`getCustomersAnalytics`
  de `DashboardAnalysis.tsx`.

- **Le filtre par canal, vérifié avant d'être copié** : le prompt affirme que
  ce filtre « existe déjà pour Produits » et demande de réutiliser le même
  mécanisme. Vérifié dans le code (`products.go`) : **faux** — Produits
  filtre par catégorie, jamais par canal ; le canal n'a jamais existé
  ailleurs dans ce paquet que comme dimension d'affichage
  (`channelCaseExpr`, utilisé uniquement en `GROUP BY` par les tabs
  CA/Commandes/TVA/Annulations), jamais en `WHERE`. Le prompt anticipait
  cette possibilité (« s'il n'est pas encore factorisé, factorise-le à cette
  occasion ») : `ChannelFilter` (`channels.go`) est donc le premier filtre
  d'entrée par canal de ce paquet, construit sur `channelCaseExpr` existant
  et le même référentiel `Channels`/`channels.ts` — disponible pour tout
  futur onglet qui en aurait besoin. Côté front, même constat :
  `ChannelFilter.tsx` (composant générique coché/décoché) existait déjà mais
  n'était câblé nulle part — c'est ici sa première utilisation réelle.

- **Les trois pièges de calcul (§3 du prompt)**, chacun avec sa propre
  vérification :
  - `customer.customer_nb_orders`/`customer_total_spent`/`last_order_date`
    ne sont jamais lus — `GetCustomersLifetimeStats` recalcule tout depuis
    `orders`. Le test d'intégration seed délibérément ces trois colonnes
    avec des valeurs fausses pour prouver que la requête les ignore.
  - « Nouveau client » = première commande **de tous les temps** tombant
    dans la fenêtre — jamais un `MIN(creation_date)` borné à la période.
    Toute agrégation par client (`first_order_date`, `last_order_date`,
    `lifetime_orders`, `lifetime_value_cents`) porte sur tout l'historique,
    de `customersEpoch` (2000-01-01) jusqu'à `periodEnd` (jamais l'horloge
    système) — un rapport sur une période passée reste reproductible.
  - `customer.creation_date` (date d'import) n'est utilisée nulle part.

- **Définitions retenues** (une ligne de justification chacune, servies
  dans le contrat lui-même — `ClientsResponse.Definitions` — pas seulement
  documentées ici) :
  - **Récurrence** : part des clients actifs sur la période (≥1 commande
    dans la fenêtre) ayant ≥2 commandes au total depuis toujours. Lecture
    "de mes clients servis cette période, combien sont des clients
    fidélisés", plus actionnable que "sur toute mon histoire, quelle part a
    commandé ≥2 fois".
  - **Segments** (nouveau/récurrent/fidèle/inactif/dormant, ordre de
    priorité strict) : nouveau (1er ordre jamais placé dans la fenêtre) >
    fidèle (actif + ≥5 commandes à vie) > récurrent (actif, <5 commandes à
    vie) > inactif (dernière commande à plus de 180 jours de la fin de
    période) > dormant (ni actif, ni assez ancien). Le bucket "dormant"
    n'existe QUE si la fenêtre demandée est plus courte que 180 jours — sur
    12 mois il est structurellement toujours à 0 (vérifié sur staging : 0).
  - **Fréquence d'achat** : commandes de la période ÷ clients actifs sur la
    période — pas l'intervalle moyen entre deux commandes (rejeté : la
    majorité des clients n'ont qu'une seule commande sur la période, un
    intervalle moyen serait indéfini pour eux et biaiserait la métrique).
  - **Inactivité** : dernière commande à plus de 180 jours, calculé à la
    date de FIN de période (pas à la date du jour) — un rapport sur une
    période passée reste reproductible s'il est rejoué plus tard.
  - **Seuil de matérialité** : 30 clients identifiés actifs sur la période,
    réutilisé **littéralement** (`minCustomersForRate =
    staffCancellationMinOrders`) depuis Annulations (PROMPT 10) plutôt
    qu'un nouveau chiffre — sous ce seuil, `RecurringRate` est `nil` et
    `SegmentRatesAvailable` est `false` ; le nombre absolu et le volume de
    référence (`IdentifiedCustomersInPeriod`) restent toujours affichés.

- **Le bug de la maquette corrigé** : `analyticsData.clients.top_clients`
  n'existait dans aucun mock (tableau mort depuis toujours) ; la ligne
  dépliée lisait `client.lifetime_value`/`client.last_visit`, deux clés
  jamais produites ; `analyticsData.clients.loyalty_score` était lu avec un
  fallback `|| 72` qui masquait silencieusement son absence permanente ;
  `analyticsService.exportClientsCSV` était appelé sans jamais être défini.
  Les quatre corrigés : `ClientsAnalyticsResponse`/`ClientsTopResponse`
  produisent réellement chaque champ consommé, `exportClientsCSV` existe
  (même patron que `exportCancellationsCSV`, CSV reconstruit côté client à
  partir des deux réponses déjà chargées).

- **Vérifié en lecture seule contre staging** (merchant 212, 12 mois,
  `POSTGRES_URL`=`RENDER_STAGING_DATABASE_URL`, `DB_DIALECT=postgres`, outil
  jetable supprimé après usage) :
  - couverture globale 45,51% (4 378/9 620) ; **Wello Resto seul : 12,30%**
    (735/5 977, proche du 86% non-identifié cité par le prompt) contre
    **marketplaces : 100,00%** (3 643/3 643) — les deux sommes reconstituent
    exactement le total non filtré (4 378 et 9 620), le filtre canal
    partitionne bien sans recouvrement ni perte ;
  - nouveaux clients recalculés à la main (requête SQL indépendante,
    n'appelant aucune fonction du dépôt) : 3 115, identique au chiffre
    produit par `GetCustomersLifetimeStats` ;
  - valeur vie du client le plus rentable (`customer_id=781`, 198 160
    centimes / 40 commandes) et d'un client médian (`customer_id=7240`,
    1 919 centimes / 1 commande) : les deux concordent au centime avec un
    `SUM(price)`/`COUNT(*)` indépendant sur `orders` ;
  - un `merchant_id` inexistant renvoie des zéros propres (`0` client,
    `0`/`0` couverture), aucune erreur ;
  - segments sur cette fenêtre : nouveau 3 115, fidèle 76, récurrent 150,
    inactif 1 010, dormant 0 (attendu, fenêtre > 180 jours) — taux de
    récurrence 523/3 341 = 15,65%.

- **Test d'intégration** :
  `internal/modules/analytics/clients_postgres_integration_test.go`
  (`-tags postgres_integration`), même patron que les 7 autres onglets —
  seed délibérément les compteurs `customer` avec des valeurs fausses,
  6 profils clients couvrant les 5 segments + une commande annulée (exclue)
  + une commande sans client (comptée en couverture, jamais en profil) +
  un canal marketplace pour vérifier la partition par canal. Exécuté avec
  succès contre staging (`go test -tags postgres_integration
  ./internal/modules/analytics/... -run TestClients_Postgres`), ainsi que
  la suite complète des 7 autres tabs (aucune régression). Tests unitaires
  (`clients_test.go`) : `ChannelFilter`, précédence des 5 segments (dont la
  découverte que "dormant" est inatteignable sur une fenêtre ≥180 jours),
  `AvgBasketTTCCents` jamais une division par zéro.

### PROMPT 16 — onglet Produits (2026-09-05)

- **Contexte** : sixième onglet analytics branché en SQL direct (après CA,
  Commandes, Règlements, TVA, Annulations). Contrat complet dès maintenant —
  coût et marge inclus — mais leur valeur reste `null` tant que les prix
  d'achat des composants ne sont pas saisis (règle posée au lot 1, PROMPT 07 :
  `orderitems.cost_price_unit`/`cost_price_reason`, jamais un `0` silencieux).
  Backend : `internal/modules/analytics/{models,products,service,handler}.go`,
  route `POST /analytics/products` (`permission.ReportsSalesRead`, même porte
  que les 5 autres onglets). Frontend : `ProductsAnalyticsTab.tsx` (wello-back-office),
  remplace le mock `renderProductsTab`/`getProductsAnalytics` de
  `DashboardAnalysis.tsx`.

- **Défauts de la maquette corrigés** (docs/analytics/AUDIT.md, wello-back-office,
  Onglet 3 + I9) : tableau tronqué à 10 lignes en dur avec tri client → vraie
  pagination serveur (`page`/`page_size`, `COUNT(*) OVER()` dans la même passe
  que l'agrégation, jamais une deuxième requête juste pour compter) et tri
  résolu en SQL (`sort_by` ∈ quantity/revenue_ttc/margin, whitelist jamais
  interpolée dans `ORDER BY`) ; liste de catégories codée en dur
  (`entrees/plats/desserts/boissons`) → lue depuis `productcateg`
  (`GetProductCategories`), incluse dans la réponse (`available_categories`),
  jamais une deuxième requête frontend séparée.

- **Le piège de l'agrégation partielle (§ du prompt)** : ne jamais diviser un
  CA complet par un coût partiel. Appliqué à deux niveaux — par produit,
  `MarginCents`/`MarginPercent` sont dérivés UNIQUEMENT de
  `CostKnownRevenueTTCCents` (le sous-ensemble des lignes à coût connu), jamais
  de `RevenueTTCCents` (le total du produit) ; en agrégat,
  `ProductsCostCoverage` réutilise **littéralement**
  `coversCoverageThreshold` (20%, déjà adopté pour
  `OrdersPeriodTotals.CoversDataAvailable`) — sous ce seuil de couverture,
  aucune marge agrégée n'est renvoyée, seulement `revenue_ttc_cents_covered`/
  `revenue_ttc_cents_total`/`coverage_ratio`, pour que le front puisse encore
  dire « marge connue sur X% du CA » plutôt que de n'afficher rien du tout.

- **Performance (§4)** : la requête de l'audit (« M2 — Mix produits + HT +
  suppléments ») mesurait 1 347 ms pour 120 433 lignes lues / 1 239 rendues —
  cinq parcours séquentiels. `GetProductsPage` lit `orderitems` une seule fois
  (un CTE agrège tout, `COUNT(*) OVER()` fournit le total de pagination dans
  la même passe) ; `GetProductsScopeTotals` est une deuxième passe, non
  groupée, pour les tuiles KPI et la couverture agrégée ;
  `GetProductsPreviousRevenue` est borné aux `product_id` de la page courante
  (jamais tout le catalogue). Mesuré en lecture (aucune écriture) contre
  staging, merchant 212, fenêtre 12 mois : `GetProductsPage` ~700-900 ms,
  `GetProductsScopeTotals` ~500 ms-1,3 s — sous les 4 s du fusible
  (`AnalyticsStatementTimeoutMS`), migration 087 (index `idx_orders_merchant_creation`,
  `idx_orderitems_order_id`, `idx_extra_order_item_id`) déjà en place.

- **Découverte en vérifiant le § « les deux onglets doivent raconter la même
  histoire »** : sur staging, merchant 212, 12 mois, la somme du CA TTC par
  produit (`orderitems`) et le total de l'onglet CA (`orders.price`) NE
  reconcilient PAS sur le périmètre complet (187 311,98 € vs 159 875,40 €,
  écart de 27 436,58 €). Tracé précisément :
  - **WELLO_RESTO seul reconcilie quasi exactement** (écart résiduel de
    -1 176,40 €, expliqué à moins de 21 € près par `orders.delivery_fees`
    (1 197,00 €) — un écart structurel déjà documenté ailleurs (VATResponse,
    même fichier) : les frais de livraison n'apparaissent jamais dans
    `orderitems`, uniquement sur `orders.delivery_fees`.
  - **L'anomalie réelle est Uber Eats** : la somme `orderitems` dépasse
    `orders.price` de **59%** sur ce périmètre (92 055,59 € vs 57 959,36 €,
    3 079 commandes) — un écart massif, pas un artefact d'arrondi.
  - **Deliveroo** est sous-évalué d'environ moitié (5 174,10 € vs
    10 657,35 €) ; en partie des lignes à prix négatif trouvées sur quelques
    commandes (9 lignes, -508,60 € au total sur toute la fenêtre) mais cela
    n'explique qu'une fraction de l'écart — la cause complète n'a pas été
    creusée davantage (chemin d'écriture Deliveroo hors périmètre de ce
    prompt).
  - **Décision (validée avec l'utilisateur, 2026-09-05)** : ne pas retarder
    la livraison de l'onglet pour investiguer/corriger le chemin d'écriture
    Uber Eats/Deliveroo — explicitement hors périmètre de ce prompt (« Ne
    touche pas... au chemin d'écriture »). L'onglet est livré tel quel, avec
    la formule du prompt (`oi.price + extra` par ligne, seule façon
    d'obtenir un CA PAR PRODUIT — `orders.price` est un scalaire par
    commande, structurellement non décomposable). Le désaccord CA
    tab/Produits tab pour les commandes marketplace est un **fait sur les
    données découvert par ce travail**, pas une régression introduite par
    lui (vérifié : la somme indépendante recalculée directement en SQL
    concorde au centime avec `GetProductsScopeTotals`). À traiter comme un
    chantier de fiabilisation du chemin d'écriture Uber Eats/Deliveroo
    séparé, pas comme un défaut de l'onglet Produits.

- **Vérifié en lecture seule contre staging** (merchant 212, 12 mois,
  `POSTGRES_URL`=`RENDER_STAGING_DATABASE_URL`, `DB_DIALECT=postgres`) :
  quantité/CA recalculés indépendamment en SQL brut concordent au centime
  avec `GetProductsScopeTotals` ; aucune ligne `orderitems.merchant_id` ne
  diffère de `orders.merchant_id` sur le périmètre (écarte une fuite
  cross-merchant via la jointure sur `order_id`) ; sur cette fenêtre,
  0 ligne `NO_RECIPE`/`INCOMPLETE_RECIPE` — la couverture de coût mesurée
  est 0% (tous les `cost_price_unit` sont NULL, cohérent avec « 85% des
  lignes NULL sur l'historique » cité par le prompt, ici 100% sur ce
  périmètre précis faute de recettes chiffrées pour ce marchand).

- **Test d'intégration** :
  `internal/modules/analytics/products_postgres_integration_test.go`
  (`-tags postgres_integration`), même patron que les 5 autres onglets —
  seed direct de `cost_price_unit`/`cost_price_reason` (pas de recette
  seedée : ce endpoint ne fait que lire ces deux colonnes, leur calcul est
  testé ailleurs, `order_life_cycle/cost_postgres_integration_test.go`).
  Couvre : NULL jamais 0 (produit NO_RECIPE/INCOMPLETE_RECIPE), garde
  d'agrégation partielle par produit, seuil de matérialité agrégé via le
  filtre catégorie, pagination + tri serveur (NULLS LAST sur marge),
  évolution nil quand pas de vente période précédente, période vide sans
  erreur. Exécuté avec succès contre staging (`go test -tags
  postgres_integration ./internal/modules/analytics/... -run
  TestProducts_Postgres`), ainsi que la suite complète des 5 autres tabs
  (aucune régression).

### PROMPT 11 — solder l'instrumentation du chemin d'écriture (2026-09-04)

- **Contexte** : suite du lot 1 (`feature/write-path-instrumentation-lot1`) et
  de l'onglet Annulations (PROMPT 10). Branche dédiée
  `feature/write-path-instrumentation-lot2`. Quatre chantiers : collision de
  numéro de migration, vérification du chemin d'écriture de
  `cancelled_by_type`, coût figé pour les options/suppléments, fiabilisation
  de `deletion_reason_id`. Aucune touche au frontend, à la couche analytique
  ni à la logique fiscale ; aucune suppression de colonne ; aucun
  rétro-remplissage de coût.

#### Découverte préalable : le lot 1 n'était pas perdu, seulement jamais committé

- `git stash list` portait `stash@{1}` : *"On feature/write-path-instrumentation-lot1:
  Lot1 write-path-instrumentation WIP (uncommitted)"*. La branche elle-même
  ne portait aucun commit propre (même HEAD que `staging`). Le stash contenait
  l'implémentation complète du lot 1 (migration, `internal/costing`,
  `order_life_cycle/{cost,cancellation,source}.go`, test d'intégration coût,
  audit TVA, câblage repository/service) — vérifiée contre
  `RENDER_STAGING_DATABASE_URL` : la migration 114 avait bien été appliquée
  pour de vrai (colonnes réelles, chiffres de rétro-remplissage strictement
  identiques à ceux du brief). Verdict pour le §2 du prompt : le chemin
  d'écriture existe et est correct, il n'était simplement pas en ligne —
  construit, validé sur staging, puis la branche abandonnée en cours de route
  au lieu d'être committée et fusionnée (hypothèse 1 du prompt, avec plus de
  nuance que « personne n'a écrit le code »). Restauré sur la nouvelle branche
  via `git stash apply` (stash conservé, pas `pop`, jusqu'à confirmation) —
  résolution manuelle de `docs/decisions.md` et
  `send_invoice_email_test.go`, tous deux modifiés en parallèle côté
  Annulations avec exactement le même correctif (vérifié par diff, pas de
  vrai conflit).

#### §1 — Collision de numéro de migration

- Renommage : `114_permission_reports_staff_performance_read` (jamais
  appliquée, encore non trackée par git) → **115** (`114_write_path_instrumentation`
  du lot 1, déjà appliquée sur staging, garde son numéro). Références mises à
  jour (`keys_gen.go`, `analytics/models.go`, `docs/decisions.md`).
- **Trois autres collisions préexistantes trouvées** en vérifiant
  l'intégralité de `migrations/todo` et `migrations/done` (regroupement par
  numéro + slug distinct, pas juste par numéro — up/down partagent
  légitimement le même numéro) : `103_permission_catalog_lot10` vs
  `103_production_ready_delivery_arrival` (`todo/`, toutes deux déjà
  appliquées — RBAC lot 10 et les colonnes de livraison sont des
  fonctionnalités vivantes) ; `024_add_planning_weeks_published_at` vs
  `024_add_users_last_login_at` et `033`/`062` de même (`done/`, archive
  MySQL gelée). Aucune renommée : toutes déjà appliquées, renommer
  réécrirait une histoire déjà déployée pour aucun bénéfice. Devenues
  l'allowlist documentée du test de garde ci-dessous.
- **Test de garde** : `migrations/migrations_numbering_test.go`
  (`TestNoDuplicateMigrationNumbers`, package `migrations` — même patron que
  `keys_gen_test.go`/`routes_rbac_permission_coverage_test.go`). Scanne
  `migrations/todo` et `migrations/done` séparément, groupe par numéro,
  échoue si plus d'un slug distinct partage un numéro, avec l'allowlist des 4
  collisions historiques ci-dessus. Vérifié qu'il détecte réellement une
  collision (retrait temporaire de `103` de l'allowlist → échec confirmé,
  restauré).

#### §2 — `orders.cancelled_by_type`

- Chemin d'écriture restauré (lot 1, stash) : `classifyCancelledByType`
  (`order_life_cycle/cancellation.go`) câblé sur `DenyOrderLocal`/
  `DeleteOrderLocal` (signature `userID` ajoutée) et le webhook direct Uber
  Eats `orders_repo.go:CancelOrder`.
- **Trois trous supplémentaires trouvés** en grep-ant tout `state = 'CLOSED'`
  du dépôt (le recensement du lot 1 — 16 chemins → 2 chokepoints + 1 bypass —
  n'était pas exhaustif) et corrigés :
  - `deliveroo_orders/repository.go:UpdateOrderRejected` — webhook de rejet
    Deliveroo, jamais touché par cancelled_by_type.
  - `ubereats/repository.go:SyncOrderState` (transition DENIED/CANCELED
    seulement, DELIVERY_FAILED exclu — même frontière que le lot 1) et
    `HandleOrderNotFound` (404 Uber) — les deux chemins de réconciliation
    appelés par `RecoverOrderState` après l'échec d'un appel API sortant
    (Deny/Cancel/SetReady/Accept).
  - Les trois utilisent une garde `cancelled_by_type IS NULL` plutôt qu'une
    écriture inconditionnelle : `SetOrderDenied`/`DeleteOrder`
    (order_life_cycle) écrivent `DenyOrderLocal`/`DeleteOrderLocal` (STAFF) de
    façon **synchrone**, avant de déclencher l'appel API externe de façon
    asynchrone — si celui-ci échoue et déclenche une réconciliation, elle ne
    doit pas écraser l'attribution STAFF déjà posée. Découverte notable en
    creusant ce point : **un chemin d'annulation staff pour Deliveroo existe
    bel et bien** dans ce dépôt (`order_life_cycle/service.go` →
    `deliverooSvc.DenyOrder`), contrairement à ce que le rétro-remplissage de
    la migration 114 supposait (« aucun chemin d'annulation staff n'existe
    pour Deliveroo ») — non corrigé rétroactivement (aucun rétro-remplissage
    dans ce lot), signalé pour référence future.
- **Bug préexistant trouvé en testant, sans rapport avec cancelled_by_type
  mais sur la même fonction** : `SyncOrderState` ne fonctionnait jamais sur
  Postgres dès que `reasonID.Valid` (cas DENIED/CANCELED précisément) — pgx
  ne sait pas encoder un `sql.NullInt64` dans une colonne `varchar`
  (`deletion_reason_id`), erreur avalée silencieusement par l'appelant
  (`RecoverOrderState` ignore l'erreur de `FinishOrderIfDoesNotExist`). Trouvé
  par le nouveau test d'intégration (l'ancien n'exerçait jamais ce chemin
  qu'avec `reasonID` vide). Corrigé au même endroit : conversion explicite en
  chaîne avant l'appel, signature publique inchangée.
- Tests : `cancellation_test.go` (table-driven, tous les sentinels de
  `classifyCancelledByType`) ; `cancellation_postgres_integration_test.go`
  (`DenyOrderLocal`/`DeleteOrderLocal`, staging réel, chaque sentinel →
  valeur attendue) ; assertions ajoutées aux tests d'intégration existants de
  `webhook/ubereats/repository`, `modules/ubereats` (nouveau test dédié
  couvrant aussi le cas « déjà classé, ne pas écraser ») et
  `webhook/deliveroo_orders`.

#### §3 — Coût figé des options et des suppléments

- Migration 116 ajoute `cost_price_unit integer`/`cost_price_reason
  varchar(20)` à `order_item_configuration` et `extra`, même vocabulaire
  `CHECK` que le lot 1 (`NO_RECIPE`/`INCOMPLETE_RECIPE`, constantes
  `costing.Reason*` réutilisées telles quelles). Aucun rétro-remplissage.
- **Découverte staging avant d'écrire le code** :
  `configurable_attribute_options.component_id` est **100 % NULL**
  (3140/3140) — la liaison option→ingrédient (migration 079) n'a jamais été
  utilisée depuis l'admin marchand. Conséquence assumée, pas un bug : tant
  qu'aucune option n'est liée, toute ligne `order_item_configuration` gèle
  `NULL`/`NO_RECIPE` — reflet correct de l'état réel, pas un défaut de
  résolution. `extra.component_id` est `NOT NULL` (toujours un ingrédient
  réel) et n'a pas de colonne `unit_of_measure` propre : `extra.quantity`
  (défaut 1, jamais fixé explicitement par le chemin d'écriture actuel)
  compte directement des unités de `purchase_price_quantity` du composant, pas
  de conversion nécessaire.
- **Options** : `resolveOptionCostsBatch` (lot 1, déjà batché par commande)
  réutilisé tel quel — le seul cas `ok=true, costCents=0` qu'il produit est
  « aucun composant lié » (`costing.PricePerUnit` rejette déjà tout prix
  ≤ 0), donc directement interprétable comme `NO_RECIPE` sans ambiguïté.
- **Suppléments** : nouveau `resolveExtraCostsBatch` (`cost.go`), même patron
  de batching, plus simple (pas de conversion d'unité).
- **Pas de requête dupliquée** : `resolveOrderItemCostsForOrder` (chemin
  `CreateOrder`) retourne désormais aussi la map `optionCosts` déjà calculée,
  réutilisée pour geler `order_item_configuration` sans reformuler la requête
  `configurable_attribute_options`. Une seule requête neuve ajoutée par
  commande (le batch extras). `UpdateOrder` résout par ligne (même asymétrie
  que le lot 1 pour `orderitems` — peu de lignes éditées à la fois).
- Tests : `cost_options_extras_postgres_integration_test.go` — coût figé
  d'une option liée et d'un supplément, non affecté par un changement
  ultérieur de `components.purchase_price` (le test central de ce lot) ;
  composant sans prix → `NULL`/`INCOMPLETE_RECIPE`, jamais `0` ; option non
  liée → `NULL`/`NO_RECIPE`, jamais `0`.
- Mesuré sur staging (méthodologie du lot 1, latence dev→Render dominante) :
  la requête neuve (`resolveExtraCostsBatch`) coûte ~25,6 ms en moyenne sur 10
  essais, contre ~26,5 ms pour un aller-retour trivial (`SELECT 1`) — confirme
  qu'elle ajoute exactement un aller-retour réseau, rien de plus, cohérent
  avec le ~21 ms/aller-retour mesuré par le lot 1. `CreateOrder` passe donc
  d'environ ~44 ms (lot 1, panier de 5 lignes) à environ ~70 ms dans ces
  mêmes conditions dev→Render — sur-estime largement l'impact réel en
  production (service et DB colocalisés), même réserve que le lot 1.

#### §4 — `deletion_reason_id`

- **Guillemets parasites : déjà mort dans le chemin d'écriture actuel**,
  vérifié contre les données et pas seulement en lisant le code. 212 lignes
  historiques (sept. 2025–févr. 2026, concentrées sur un compte staff) portent
  des guillemets littéraux ; **zéro** ligne après le 2026-02-18, y compris des
  annulations récentes du même compte staff, n'en porte. Aucune logique de
  concaténation de guillemets trouvée dans le chemin Go actuel
  (`handler.go` → `service.go` → `DenyOrderLocal`/`DeleteOrderLocal` passe
  `req.DeletionReasonID` tel quel en paramètre lié). Commit exact du correctif
  non retrouvé (recherché dans l'historique `wello_resto_flutter` autour de
  la date, sans résultat concluant) mais la donnée est concluante : rien à
  corriger côté code d'écriture pour ce point précis.
- **Ce qui est encore vivant et cassé** : `varchar(11)` était trop étroit
  pour les constantes de ce dépôt lui-même (`KIOSK_CUSTOMER_CANCELLED` = 24
  caractères, `SNO_CUSTOMER_CANCELLED` = 23). Confirmé par sonde directe
  contre staging (table temporaire) qu'une écriture paramétrée normale
  **échoue** sur `varchar(11)` en cas de dépassement (`value too long`), sans
  troncature silencieuse — pourtant des lignes tronquées récentes
  (`KIOSK_CUSTO`, jusqu'au 2026-07-23) existent en base. Mécanisme exact non
  élucidé (aucun trigger/règle sur `orders`, aucune troncature côté Go
  trouvée) — vraisemblablement un chemin d'écriture hors de ce dépôt.
  Corrigé quand même à la source la plus sûre : migration 116 élargit
  `orders.deletion_reason_id` à `varchar(32)` (marge au-dessus des 24
  caractères actuels).
- Migration 117 (**écrite, non appliquée**, conformément à la consigne) :
  nettoie les 212 lignes à guillemets (`trim(both '''' from ...)`, même
  motif que le rétro-remplissage du lot 1).

#### Validation

- `go build ./...`, `go vet ./...` : mêmes avertissements préexistants, sans
  rapport (ubereats/client code inatteignable, verrou copié dans
  auth/handler.go).
- `go test ./...` sur l'arbre entier : mêmes 4 échecs préexistants que ceux
  déjà catalogués par le lot 1 (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) — zéro régression neuve.
- Suite `postgres_integration` complète sur les paquets touchés : verte, à
  une exception préexistante et hors périmètre — `orders.brand_store_id`
  (ajoutée par la migration 111, déjà committée, jamais appliquée sur cette
  instance staging) fait échouer les trois tests qui touchent
  `GetOrderMetadata`/`CreateOrder` côté Uber Eats/order_life_cycle ; non
  appliquée ici (hors périmètre de ce lot), signalé pour qui gère le
  déploiement staging.
- Un défaut de fixture de test préexistant trouvé et corrigé en cours de
  validation (hors périmètre mais nécessaire pour obtenir un run vert) :
  `webhook/deliveroo_orders/postgres_integration_test.go` seedait/attendait
  encore `brand_status` en minuscules pour `UpdateOrderAccepted`/
  `UpdateOrderConfirmed`, jamais mis à jour quand le lot 1 a normalisé
  l'écriture/la comparaison en majuscules (B3) — fixture corrigée, aucune
  logique applicative touchée.
- Migrations 115 et 116 appliquées sur staging pour de vrai (additives,
  idempotentes) — chiffres ci-dessus mesurés après application réelle.
  Migration 117 volontairement non appliquée.

### PROMPT 10 — onglet Annulations : périmètre, deux endpoints, `reports.staff_performance.read` (2026-09-04)

- **Livrable** : `internal/modules/analytics/{scope,cancellations,models,service,handler}.go`,
  migration `115_permission_reports_staff_performance_read` (renumérotée
  depuis 114 par PROMPT 11 — collision avec `114_write_path_instrumentation`
  du lot 1, resté non fusionné/non numéroté au moment où ce lot a pris le
  numéro ; voir l'entrée PROMPT 11 plus bas), constante
  `permission.ReportsStaffPerformanceRead`, câblage `cmd/api/routes.go`,
  tests unitaires + `TestCancellations_Postgres` (lecture seule, staging).
  `POST /analytics/revenue|orders|payments|vat` non touchés.
- **Périmètre d'annulation retenu : `brand_status = 'CANCELED'` seul**, pas
  `CANCELED+DENIED+DELETED` (1 738 lignes, périmètre PROD, tout historique —
  le chiffre cité par le prompt). Sous `CANCELED` seul : 1 461 lignes sur le
  même tout-historique PROD (248 `DENIED` + 29 `DELETED` exclus). Tracé dans
  le code qui
  écrit chaque statut (`internal/modules/order_life_cycle/repository.go`,
  `internal/modules/ubereats/repository.go`, `internal/modules/deliveroo/
  repository.go`) : `DENIED` est un refus **à la prise**, jamais après
  acceptation (`cancelled_by_type` y est exclusivement `SYSTEM`/`PLATFORM`,
  jamais `STAFF`/`CUSTOMER` — vérifié sur staging) ; `DELETED` est du code
  mort côté écriture (`git log --all -S "brand_status='DELETED'"` : aucun
  résultat, sur aucune branche) — plus aucune ligne depuis le 2024-10-26,
  `DeleteOrderLocal` écrit désormais `CANCELED`. Documenté en tête de
  `scope.go` (`AnalyticsCancellationsScope`), sans filtre `state` (les
  chemins d'écriture plateforme ne touchent jamais `orders.state`).
- **Dénominateur du taux retenu : annulées ÷ toutes commandes créées** (pas
  ÷ annulées+valides — `AnalyticsOrdersScope` exclut déjà les annulations par
  construction, ce qui aurait obligé à redéfinir "valide" ad hoc rien que
  pour ce ratio). `AnalyticsAllOrdersCreatedScope` (scope.go) est la seule
  définition. Le backend n'émet jamais de taux pré-divisé — seuls
  `cancelled_count`/`total_orders_created` sont renvoyés, le front calcule
  l'affichage, comme `OrdersPeriodTotals` le fait déjà pour les couverts.
- **Deux endpoints, une seule clé nouvelle** : `POST /analytics/cancellations`
  reste sous `reports.sales.read` (même groupe `r.Use` que les 4 autres
  onglets) ; `POST /analytics/cancellations/by-staff` est déclaré **hors** de
  ce groupe, avec son propre `r.Use(RequirePermission(ReportsStaffPerformanceRead))`
  — `RequirePermission` ne prend qu'une seule clé (RBAC lot 2), empiler les
  deux aurait exigé les deux droits à la fois pour la même route.
- **`reports.staff_performance.read` (migration 115, sort_order 135,
  `is_sensitive=true`)** : recette DROITS.md §5.3 suivie à la lettre —
  constante + `All` dans `keys_gen.go`, câblée sur `/by-staff`,
  `TestAllMatchesMigrationCatalog`/`TestRBACPermissionCoverage`/
  `TestRBACRatchet` verts. **Pas d'entrée `legacyPermissionFallback`**
  (deliberate, comme les 5 clés du lot 10) : en production (`role_id` encore
  NULL partout), cette route retombe de facto à `Rights.Admin` seul tant que
  `cmd/seed_system_roles` + l'attribution de rôles n'ont pas tourné —
  documenté ici pour ne pas être une redécouverte surprise (même remarque
  que RBAC lot 11 Phase 5 pour lot 10).
  **Reste à faire, hors périmètre lecture seule de ce lot** : déployer la
  migration 115 puis lancer `cmd/seed_system_roles` sur staging pour que le
  rôle Administrateur porte la nouvelle clé (non fait ici — aucune écriture
  sur staging n'a été autorisée pour ce prompt).
- **`cancelled_by_type` : chaîne d'écriture introuvable dans ce dépôt.**
  Vérifié : `git log --all -S "cancelled_by_type"` → zéro résultat, sur
  toutes les branches, tout l'historique. Pourtant la colonne est réelle,
  vivante, et croît en continu jusqu'aux commandes les plus récentes
  (couverture ~0% mi-2025 → ~100% depuis fin 2025, aucun trigger ni fonction
  Postgres ne la référence). Le code qui l'alimente n'est dans aucun dépôt
  local accessible. Fiable pour construire dessus (donnée vivante, pas un
  backfill figé) mais à signaler à qui maintient l'écriture réelle.
  **Résolu par PROMPT 11** (voir plus bas) : `git log --all -S` ne pouvait pas
  le trouver car `--all` ne couvre pas `refs/stash` — le code existait déjà,
  complet et validé sur staging par le lot 1, mais était resté dans une stash
  jamais poppée ni committée sur `feature/write-path-instrumentation-lot1`.
- **Deux bugs qualité découverts sur `orders.deletion_reason_id`**, tous deux
  gérés en le traitant comme un **identifiant potentiellement sale**, jamais
  en supposant une jointure propre vers `deletion_reasons` (entier) :
  guillemets parasites stockés dans la valeur elle-même (ex. la chaîne
  littérale `'3'`, apostrophes comprises) et troncature (`varchar(11)`, un
  code observé coupé net à 11 octets, ex. `KIOSK_CUSTO`). `GetCancellationsByReason`
  (cancellations.go) trim les guillemets avant jointure et route tout ce qui
  ne matche aucune ligne du référentiel vers un bucket `uncatalogued:<brut>`
  explicite plutôt que de le faire disparaître silencieusement — même
  principe que le bucket `UNKNOWN` de `cancelled_by_type`.
- **Motifs dérivés des commandes réellement annulées, jamais du référentiel
  `deletion_reasons` énuméré à part** : `deletion_reasons` est global et
  partagé avec les réservations/livraisons (DROITS.md/PERIMETRE.md) ; en
  groupant `GetCancellationsByReason` depuis `orders` puis en joignant VERS
  le référentiel, un motif de réservation ne peut structurellement jamais
  apparaître dans cet onglet — pas besoin d'une liste blanche
  `deletion_reason_object`.
- **Bloc nominatif** : seuil d'effectif = **30 commandes créées sur la
  période** avant d'afficher un taux (comptes bruts sinon, toujours visibles).
  Vérifié sur staging (PROD, 12 mois) : seulement 7 couples
  (établissement, serveur) portent la moindre annulation `STAFF`, la plupart
  des établissements tournant sur un compte POS partagé plutôt qu'un compte
  par salarié (`Users` par établissement très faible, PERIMETRE.md). Les
  annulations `STAFF` non rattachables à un `users.user_id` réel (~11 % sur
  le périmètre PROD complet, 12 mois ; 0 sur le seul merchant 212) forment une ligne
  synthétique `unattributed` plutôt que de disparaître — condition posée par
  le prompt §6 : la somme des `cancelled_count` du endpoint nominatif doit
  égaler exactement le sous-total `STAFF` de l'agrégat, vérifié sur staging.
- **Vérifié en lecture seule contre staging, merchant 212, 12 mois
  (2025-09-04→2026-09-04)** : `cancelled_count=830`, `total_orders_created=
  10 474` (taux 7,92 %), `cancelled_amount_cents=1 567 382`. Motifs/canaux/
  typologies d'auteur somment chacun exactement à 830. Endpoint nominatif :
  `Σcancelled_count=775` = sous-total `STAFF` de l'agrégat (775), et 100 %
  des annulations `STAFF` de ce merchant sont rattachables à un utilisateur
  réel (2 comptes : `226`→774, `2`→1). `unknown_cancelled_count=2/830`
  (0,24 %) et `with_reason=825/830` (99,4 %) — très en-dessous du chiffre
  « 13,4 % de NULL » cité par le prompt : recalculé sous ce périmètre
  (`CANCELED` seul, pas `CANCELED+DENIED+DELETED`), pas repris tel quel.
  Établissement à zéro annulation sur la période (merchant 225, 3 mois) :
  zéros propres, aucune erreur.
- **Verdict coût des options (vérification préalable du prompt)** :
  `orderitems.cost_price_unit`/`cost_price_reason` existent (le snapshot
  produit du lot 1 est bien posé), mais `extra` (table des options/suppléments
  vendus) **n'a aucune colonne de coût** — ni `cost_price_unit` ni
  équivalent, seulement `component_id` pointant vers `components.purchase_price`
  (prix d'achat **courant**, pas un instantané à la vente). Comme
  `cancelled_by_type`, ni l'une ni l'autre de ces deux colonnes de coût
  n'a de migration ni de code d'écriture dans ce dépôt (même vérification
  `git log --all -S`, zéro résultat) — posées hors bande, comme `cancelled_by_type`.
  Le coût des options reste donc un chantier séparé et n'a pas été codé ici,
  conformément à la consigne du prompt.

### PROMPT 08 lot 2 — RBAC : retrait des deux derniers lecteurs directs de `Rights.Admin` (2026-09-04)

- **Contexte** : ferme le chantier ouvert par le prompt 05. Sur les dix
  méthodes sœurs de `UserLoginRow` (`HasMenuAccess`, `HasSettingsAccess`,
  etc.), deux n'avaient jamais été migrées vers `Has()`/le catalogue RBAC et
  lisaient encore `Rights.Admin` en direct : `HasAccessReception()` et
  `CanPrintCashReport()` (`internal/modules/auth/models.go`). Décisions
  reçues telles quelles (pas de nouvelle analyse d'opportunité) : la première
  est supprimée, la seconde décommissionnée. Branche dédiée
  `feature/rbac-lot12-decommission-legacy-checks`, partie de `staging`.
- **Recensement, avant toute suppression** : grep exhaustif des deux noms de
  méthode sur tout le dépôt Go.
  - `HasAccessReception()` : **zéro appelant**, nulle part — ni route, ni
    champ de réponse. Confirmé mort avant même ce lot (un contrôle d'accès
    retiré ailleurs dans une bascule antérieure, sans que la méthode
    elle-même ait jamais été nettoyée). Suppression pure, aucun contrat à
    préserver.
  - `CanPrintCashReport()` : **un seul appelant**,
    `internal/modules/auth/service.go` (avant ce lot, ligne 470),
    alimentant `capabilities.actions.print_merchant_cash_report` dans la
    réponse de login (`LoginCapabilityActionsResponse`) — un usage
    d'**affichage** (capability envoyée au client), jamais une décision
    d'autorisation : aucune route ne l'appelait. Deux autres champs voisins
    portent presque le même nom JSON mais sont indépendants et non
    touchés : `access.permissions.print_merchant_cash_report`
    (`LoginAccessPermissionsResponse`, lit `Rights.PrintMerchantCashReport`
    brut, sans `Admin`, commentaire déjà explicite en place depuis RBAC lot
    6) et `legacy.print_cash_report` (`LoginLegacyFields`, payload déprécié,
    même lecture brute).
- **Vérification client, avant de toucher au contrat de login** : recherche
  dédiée dans les 4 dépôts front (`wello_resto_flutter`, `wello-kiosk`,
  `wello-resto-scannorder`, `wello-back-office`) pour
  `print_merchant_cash_report`. Résultat : **`wello_resto_flutter`**
  (`AuthCapabilityActionsResponse`/`AuthCapabilityActionsDto`, y compris un
  getter deprecated qui pointe explicitement vers
  `capabilities.actions.printMerchantCashReport`) et **`wello-back-office`**
  (`AuthCapabilities.actions.print_merchant_cash_report` dans
  `src/types/auth.ts`) parsent et stockent ce champ ; aucun des deux ne s'en
  sert pour gater une action UI à ce jour (recherché explicitly, rien
  trouvé), mais **le champ est bien consommé** (parsé/stocké) — condition
  suffisante pour ne pas le retirer du contrat. `wello-kiosk` et
  `wello-resto-scannorder` : aucune référence, pas concernés. Un homonyme
  sans rapport existe côté back-office (`MerchantUserPermissions.print_merchant_cash_report`,
  l'endpoint admin `/users/{id}/rights` de gestion des droits employé — un
  contrat différent, non touché par ce lot).
- **Appliqué** :
  - `HasAccessReception()` supprimée (`internal/modules/auth/models.go`).
  - `CanPrintCashReport()` supprimée également (le corps aurait été
    `return true`, aucun intérêt à garder un wrapper) ; son unique site
    d'appel remplacé par la constante `true` directement, avec commentaire
    renvoyant à cette entrée — exactement la remédiation « valeur constante
    en attendant la mise à jour du client » demandée par le brief plutôt que
    de supprimer le champ JSON. `access.permissions.*` et `legacy.*` restent
    des lectures brutes de `Rights.PrintMerchantCashReport`, inchangées.
  - Repli `pos.status.manage` (`legacyPermissionFallback[permission.POSStatusManage]`,
    `internal/modules/auth/permissions.go:23`, backé par la colonne
    `users_rights.access_wrreception` via le champ Go `AccessReception`)
    vérifié **toujours présent et intact** — fichier non touché par ce lot,
    confirmé par `git diff` vide sur `permissions.go` et par les tests
    `TestRequirePermission_POSStatusManage_*`
    (`internal/middleware/require_permission_test.go`), qui passent tels
    quels. Ce repli est un champ Go différent (`AccessReception`, lu par
    `legacyPermissionFallback`) de la méthode supprimée
    (`HasAccessReception()`, qui lisait `Rights.Admin || Rights.AccessReception`
    puis n'était appelée nulle part) — deux choses distinctes qui partagent
    la même colonne source, pas la même fonction.
- **Non touché, comme demandé** : `users_rights.admin` (la colonne) reste en
  base, toujours lue par le repli `Has()` (`RoleID == nil`) et par
  `HasAdminRole()` — ce sont eux qui font tourner la production tant que
  `role_id` y est `NULL` partout (voir `docs/RBAC_REPLI_HISTORIQUE.md`).
  Migration `113_drop_users_rights_admin_column` reste préparée, **non
  appliquée**. Son commentaire d'en-tête mis à jour : elle listait
  `HasAccessReception()`/`CanPrintCashReport()` comme deux des quatre
  lecteurs bloquants — n'en reste que deux (`HasAdminRole()`'s branche
  `RoleID == nil`, `LoginLegacyFields.Admin`), ce lot en a fermé deux sur
  quatre, pas la totalité.
- **Documentation** : `docs/RBAC_REPLI_HISTORIQUE.md` (nouvelle section
  « RBAC lot 12 ») et `docs/RBAC_ROUTES.md` (note dans l'en-tête) mis à jour.
  **Le tableau des 18 clés du catalogue et le compte « 10 avec repli / 8 sans »
  ne changent pas** : les deux méthodes retirées n'étaient ni des clés
  `permission.Key` ni des entrées de `legacyPermissionFallback` — deux
  méthodes autonomes hors du système `Has()`/repli, question posée
  explicitement par le brief et tranchée par la négative.
- **Tests** : nouveau
  `TestBuildLoginResponse_PrintMerchantCashReport_AlwaysTrue`
  (`internal/modules/auth/login_response_test.go`, 4 sous-cas croisant
  `Admin`/`PrintMerchantCashReport`) — confirme la capability toujours à
  `true` et, dans le même test, que `access.permissions.print_merchant_cash_report`
  continue de refléter le droit réel de l'utilisateur (non affecté). Suite
  complète `go build ./...`/`go test ./...` : aucune régression — les 4
  échecs pré-existants sans rapport (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) inchangés ; un cinquième,
  déjà rencontré et corrigé sur le lot « instrumentation du chemin
  d'écriture » (signature `customers.NewCustomersService` désynchronisée,
  bloquait la compilation des tests d'`order_life_cycle`), corrigé ici aussi
  à l'identique puisque cette branche part aussi de `staging`.
- **Suite** : rien de planifié par ce lot — la condition de retrait du
  repli historique et de la colonne `users_rights.admin` reste celle décrite
  dans `docs/RBAC_REPLI_HISTORIQUE.md` (0 ligne `role_id IS NULL` active,
  production comprise), non atteinte, non traitée ici.

### PROMPT 07 lot 1 — instrumentation du chemin d'écriture : coût figé, source, annulation, casse (2026-09-04)

- **Contexte** : première vague de `ROADMAP-analytics.md` (doc non présent
  dans le dépôt — même situation que `docs/analytics/DROITS.md` pour le
  chantier RBAC : les figures citées par le brief ont toutes été vérifiées
  contre staging et se sont révélées exactes, donc pas de blocage sur son
  absence). Périmètre : migration additive unique
  (`migrations/todo/114_write_path_instrumentation.up/down.sql`), code
  d'écriture sur branche dédiée `feature/write-path-instrumentation-lot1`,
  audit TVA (`docs/TVA_MARKETPLACE_AUDIT.md`). Aucune touche à la couche
  analytique, à la logique fiscale ni au RBAC.
- **Recherche préalable** : 4 agents Explore en parallèle (chemin d'écriture
  `orderitems`/calcul de coût existant, recensement exhaustif des 16 chemins
  d'annulation, schéma `orders`/`customer` et usages de `brand_status`,
  payloads webhook Uber Eats/Deliveroo pour la TVA) + vérification directe
  contre `RENDER_STAGING_DATABASE_URL` à chaque étape (schéma réel,
  distribution des données, simulation SQL des règles de rétro-remplissage
  avant de les figer dans la migration).

#### B2 — coût de revient figé sur `orderitems`

- **Colonnes** : `cost_price_unit integer` (coût **unitaire**, pas coût de
  ligne — se multiplie trivialement par `quantity` à la lecture, comme
  `price`/`base_price` déjà stockés en unitaire ; un coût de ligne
  pré-multiplié aurait dû être recalculé à chaque changement de quantité) et
  `cost_price_reason varchar(20)` (`NO_RECIPE` | `INCOMPLETE_RECIPE`,
  `CHECK` constraint). Centimes entiers, jamais de flottant — `orders.monnaie`
  documenté comme contre-exemple (colonne `real`, jamais lue côté Go après
  scan, dérive silencieuse potentielle).
- **Distinction NO_RECIPE / INCOMPLETE_RECIPE** obtenue sans colonne
  supplémentaire : `cost_price_unit` NULL + `cost_price_reason` NULL = ligne
  antérieure à ce lot (jamais évaluée) ; NULL + `NO_RECIPE`/`INCOMPLETE_RECIPE`
  = évaluée après déploiement, cas normal vs défaut de paramétrage.
- **Calcul** : `internal/costing` (nouveau package, pur, sans accès DB) —
  `UnitCost`/`ConvertedQuantity`/`PricePerUnit`. La logique existante
  (`menu.GetAllProducts`) était dupliquée 3 fois et divergeait sur la
  convention `unit_of_measure_convert` : `menu.GetAllProducts` interroge
  `(id_from=unité requise, id_to=unité composant)` et divise,
  `stocks.ConsumeOrderStock` interroge la paire inverse et multiplie — les
  deux sont justes (vérifié sur staging : chaque paire existe en double sens,
  ratios bien réciproques), mais ne doivent pas être mélangées. `internal/costing`
  fixe la convention de `menu.GetAllProducts` (division) et corrige un vrai
  bug de cette dernière au passage : `COALESCE(conv.ratio, 1)` suppose
  silencieusement une conversion 1:1 quand aucune ligne de conversion
  n'existe pour des unités différentes — remplacé par un échec explicite
  (`ok=false` → `INCOMPLETE_RECIPE`), conforme à la règle NULL≠0 du lot.
  `requires.consumable_id` (17/2246 lignes actives sur staging) n'est pas
  résolu par ce lot (le brief ne mentionne que `components.purchase_price`) :
  traité comme incomplet plutôt qu'ignoré silencieusement.
- **Factorisation partielle, assumée** : `menu.GetAllComponents` et
  `stocks.GetComponentsList` (duplications triviales à une ligne,
  `purchase_price / purchase_price_quantity`) migrées vers
  `costing.PricePerUnit`. Le calcul complet de `menu.GetAllProducts`
  (agrégation multi-composants, affichage admin uniquement, jamais sur le
  chemin d'écriture) n'a **pas** été réécrit pour réutiliser `internal/costing`
  — risque de régression sur l'éditeur de menu hors scope de ce lot pour un
  gain nul sur l'objectif B2. Signalé comme nettoyage candidat pour un lot
  séparé plutôt que fait à la sauvette ici.
- **Options** : `configurable_attribute_options.component_id/quantity/unit_of_measure`
  existaient déjà (migration 079, jamais branchés sur un calcul de coût).
  Le coût d'une option sélectionnée est résolu avec le même
  `costing.UnitCost` et additionné au coût unitaire de la ligne — pas de
  nouvelle colonne. Convention alignée sur `extra_price` (déjà multiplié par
  `oi.quantity` dans les recalculs de CA existants) : le coût d'une option
  est résolu **par unité de produit**, pas par ligne.
- **Performance, mesurée avant/après correctif** : la résolution initiale
  (une par ligne de commande) coûtait ~2 allers-retours SQL par ligne
  (recherche de recette + jointure requires/components/conversion),
  mesuré à ~46 ms/ligne depuis la machine de dev vers Postgres staging —
  chiffre dominé par la latence réseau dev→Render (confirmé : une simple
  requête déjà sur le chemin critique, `GetNextOrderNum`, coûte ~21 ms dans
  les mêmes conditions), donc surestime largement l'impact réel
  service→DB colocalisés en production. Corrigé quand même, par principe
  (le nombre d'allers-retours, lui, était réel et proportionnel au nombre de
  lignes) : `insertOrderItems` (chemin `CreateOrder`) résout désormais tout
  le panier en un lot (`resolveOrderItemCostsForOrder` — 2 requêtes IN(...)
  au total, quelle que soit la taille du panier) au lieu d'une résolution
  par ligne. Mesuré après correctif : ~44 ms pour un panier de 5 lignes
  (contre ~230 ms estimés sans le lot), soit le coût figé de 2
  allers-retours au lieu de 2×N. `UpdateOrder` (édition d'une commande
  ouverte) garde la résolution ligne par ligne — édite typiquement 1-2
  lignes à la fois, pas tout un panier neuf ; changement jugé disproportionné
  pour ce lot, à reconsidérer si `UpdateOrder` s'avère un jour appelé avec
  beaucoup de lignes modifiées simultanément. Pas de cache Redis posé : le
  vrai problème était le nombre d'allers-retours (résolu par le batch), pas
  le coût de calcul lui-même.
- **Choke points touchés** : `InsertOrderItem`/`insertOrderItems` (repository.go,
  chemin `CreateOrder` — POS, kiosk, ScanNOrder, webhooks Uber Eats/Deliveroo,
  tous convergent ici) et les deux variantes de `UpdateOrder` (insert de
  nouvelle ligne + upsert MySQL/Postgres — les deux tenues synchronisées en
  nombre de placeholders, le chemin MySQL étant mort en production mais
  toujours compilé/lié au même jeu de paramètres).

#### A6 — `orders.order_source`

- **Colonne** : `varchar(30)` + `CHECK IN ('WELLO_RESTO_POS','KIOSK',
  'SCANNORDER','UBER_EATS','DELIVEROO')`. Aucune colonne existante ne porte
  déjà cette info — confirmé par grep et par deux docs internes
  (`docs/ARCHITECTURE_API.md` §7.3, `docs/KIOSK_DECISIONS.md`) qui proposaient
  déjà ce champ sans jamais l'implémenter (dette technique signalée, jamais
  traitée avant ce lot).
- **Dérivation** (`internal/modules/order_life_cycle/source.go`,
  `resolveOrderSource`, mêmes règles dans la migration pour le
  rétro-remplissage) : `brand=UBER_EATS/DELIVEROO` → valeur directe (created_by
  ne dit rien du canal pour ces deux, seulement de la commande d'origine du
  webhook) ; `brand=WELLO_RESTO` + `created_by='KIOSK'` → `KIOSK` ;
  `created_by IN ('SCANNORDER','-1')` → `SCANNORDER` (`-1` = marqueur legacy,
  déjà traité comme équivalent ailleurs dans `pos/accounting`) ; `created_by`
  numérique ou `user-<uuid>` → `WELLO_RESTO_POS`. Piège trouvé et corrigé en
  cours de route : `upsertCustomer` s'exécute **avant** `setOrderDefaults`
  dans `CreateOrder`, donc `brand` peut encore valoir `""` au moment de
  dériver l'`acquisition_source` du client — `resolveOrderSource` traite `""`
  comme `WELLO_RESTO` pour rester correct indépendamment de l'ordre d'appel.
- **Rétro-remplissage** : vérifié sur staging avant d'écrire la migration —
  33858/33862 commandes (99,99 %) couvertes sans ambiguïté ; le reste (3
  lignes) porte un `created_by` contredisant `brand` (anomalie de données
  historique, ex. `brand=WELLO_RESTO` avec `created_by=WEBHOOK_UBER_EATS`) →
  laissé `NULL`.

#### A6b — `customer.acquisition_source`

- **Colonne** : `varchar(30)`, pas de `CHECK` (vocabulaire ouvert, le brief
  ne fixe pas de liste fermée contrairement à A6). Distincte de
  `customer_brand` existant (marque propriétaire de la fiche, pas canal
  d'acquisition — confusion explicitement signalée par le brief).
- **Non rétro-remplie**, par construction. Protection en ceinture et
  bretelles : `CustomersRepository.UpdateOrCreateCustomer` exclut désormais
  `acquisition_source` de sa branche UPDATE (pas seulement laissée `nil` côté
  appelant) — impossible d'écraser la valeur captée à la création d'un client
  existant, même par erreur d'appel future.

#### C2 — `orders.cancelled_by_type`

- **Recensement exhaustif** : 16 chemins identifiés (voir l'agent de
  recherche dédié), convergeant tous vers 2 points de choke
  (`DenyOrderLocal`, `DeleteOrderLocal`) + 1 chemin direct hors
  `order_life_cycle` (webhook Uber Eats `orders_repo.go:CancelOrder`, bypass
  complet). `cancelled_by_type` dérivé de l'acteur déjà porté par
  `DenyOrderRequest.UserID`/`DenyOrderInput.UserID` (jusque-là utilisé
  seulement pour les notifications/clé Redis, jamais persisté) :
  `SYSTEM`/`WEBHOOK_STRIPE` → SYSTEM, `WEBHOOK_DELIVEROO`/`WEBHOOK_UBER_EATS`
  → PLATFORM, `SNO_CUSTOMER`/`KIOSK` → CUSTOMER, tout le reste (par
  construction : un vrai `user_id` authentifié) → STAFF. Uber Eats webhook
  direct : `PLATFORM` codé en dur (aucun acteur du tout dans ce payload).
  **Hors périmètre, assumé** : `terminalizeDeliveryStop` (échecs de livraison
  `DELIVERY_FAILED`/`DELIVERY_CANCELED`) — statuts distincts de
  DENIED/CANCELED, une livraison ratée peut être retentée, pas clairement
  une « annulation » au sens C2 ; laissé de côté plutôt que deviné.
- **Rétro-remplissage**, vérifié sur staging avant migration (2225 commandes
  annulées/refusées au total) : `brand=DELIVEROO` → `PLATFORM` systématique
  (aucun chemin d'annulation staff n'existe pour Deliveroo dans ce dépôt,
  contrairement à Uber Eats — vérifié par grep exhaustif). `brand=UBER_EATS` :
  signal décisif = `deletion_reason_id`, pas `created_by` (qui ne porte que
  l'identité du webhook créateur, jamais celle de qui annule) — reason `39`/`41`
  (catalogue `uber_eats_webhook`/`uber_eats_api`) → PLATFORM, reason dans le
  catalogue `uber_eats_cancel`/`uber_eats_deny` (12-26, 28-34 — raison Uber
  choisie par un staff pour propager l'annulation via l'API) → STAFF.
  `brand=WELLO_RESTO` : `created_by='KIOSK'` ou reason
  `KIOSK_CUSTOMER_CANCELLED`/`SNO_CUSTOMER_CANCELLED` → CUSTOMER (**bug
  préexistant découvert en vérifiant les données** : `orders.deletion_reason_id`
  est `varchar(11)`, ces deux sentinelles Go, plus longues, sont tronquées
  silencieusement à l'écriture — `KIOSK_CUSTO`/`SNO_CUSTOME` sur staging ;
  migration adaptée pour matcher les deux formes, bug lui-même **non
  corrigé** ici, hors périmètre de ce lot, signalé pour un lot séparé) ;
  reason `42`/`43` (expiration automatique d'approbation/paiement) → SYSTEM ;
  `created_by` réel (numérique ou `user-...`) + reason du catalogue générique
  `order` (motifs 1-8) → STAFF. Couverture finale : STAFF 1270, SYSTEM 485,
  PLATFORM 145, CUSTOMER 27, **NULL 298 (13,4 %, laissé tel quel plutôt que
  deviné** — essentiellement des lignes sans `deletion_reason_id` exploitable,
  legacy d'avant l'usage systématique de ce champ).
- **Artefact de données trouvé en vérifiant, corrigé dans la migration
  seulement** : des lignes historiques portent `deletion_reason_id` avec des
  guillemets littéraux dans la valeur (`"'3'"` au lieu de `"3"`, ~212 lignes)
  — `trim(both '''' from ...)` ajouté à toutes les comparaisons du
  rétro-remplissage pour les récupérer plutôt que les laisser tomber en NULL.

#### B3 — normalisation `brand_status`

- **Cause racine trouvée** : Deliveroo est le seul provider à écrire en
  minuscules, et à **deux** endroits, pas un seul — `buildOrderRequestObject`
  (`internal/webhook/deliveroo_orders/service.go`, passthrough
  `brandStatus := ord.Status` à la **création** de commande, jamais vu par
  l'audit initial du brief) et `Repository.UpdateOrderRejected`/
  `UpdateOrderAccepted`/`UpdateOrderConfirmed` (mises à jour de statut,
  littéraux `'scheduled'`/`'accepted'`/`'confirmed'` + la comparaison CASE
  elle-même). Les deux corrigés pour écrire/comparer en majuscules.
  Uber Eats et WELLO_RESTO écrivaient déjà en majuscules partout — aucun
  autre chemin d'écriture touché.
- **Backfill** : `UPDATE orders SET brand_status = upper(brand_status) WHERE
  brand_status <> upper(brand_status)` — 36 lignes sur staging (accepted:14
  canceled:11 rejected:10 placed:1, toutes `brand=DELIVEROO`), exactement le
  chiffre cité par le brief pour le périmètre total.
- **Pas de nouveau code de comparaison à corriger** : tous les filtres
  existants (`pos/accounting`, `pos/reports`, `stats`, `tasks/orders.go`,
  `tasks/payments.go`, `orders/repository.go`) comparent déjà en majuscules
  nu, sans `upper()` — corrects par construction une fois qu'aucun code
  n'écrit plus en minuscules. `analytics/scope.go` avait déjà un `upper()`
  défensif (chantier RBAC/analytics antérieur) — laissé tel quel, inoffensif.

#### B1 — audit TVA marketplace

- Constat détaillé : `docs/TVA_MARKETPLACE_AUDIT.md`. Résumé : aucun champ
  taxe dans les deux payloads webhook, `ht`/`tva` à 0 par construction (codé
  en dur côté Deliveroo, jamais assigné côté Uber Eats). Le recalcul depuis
  les lignes existe déjà et couvre les deux marketplaces, mais uniquement
  côté `internal/modules/analytics` (`pos/reports` les exclut explicitement) —
  et sa fiabilité pour ces deux marques dépend d'un mapping `tva_categories`
  qui diffère à l'auto-création de produit (Uber Eats en affecte une par
  défaut, Deliveroo non — risque de perte silencieuse de lignes côté
  Deliveroo, non quantifié). Aucun correctif posé, conformément au brief.

#### Validation

- **Corrections incidentes, hors périmètre du lot mais nécessaires pour
  vérifier le reste** : `send_invoice_email_test.go` (signature
  `customers.NewCustomersService` désynchronisée d'un chantier antérieur,
  bloquait la compilation de tout le paquet `order_life_cycle`, y compris ses
  tests d'intégration) — un seul argument `nil` ajouté, aucune logique
  touchée.
- **Migration 114 appliquée sur staging** (additive, idempotente) pour
  valider réellement le lot plutôt que sur la seule lecture du code — voir
  chiffres de rétro-remplissage ci-dessus, tous mesurés après application
  réelle, pas simulés.
- Nouveau test d'intégration Postgres
  (`cost_postgres_integration_test.go`, tag `postgres_integration`) contre
  staging : coût figé après modification de `components.purchase_price`
  (le test central du lot), `NO_RECIPE` ≠ `INCOMPLETE_RECIPE`, coût d'option
  inclus, résolution batchée strictement identique à la résolution ligne par
  ligne sur un panier mélangeant les 4 cas + un même produit sur deux lignes.
- `go build ./...`, `go vet ./...` (mêmes avertissements pré-existants,
  sans rapport, vérifiés sur l'arbre propre) et `go test ./...` diffés
  avant/après ce lot sur l'arbre entier : **zéro régression** — les 4
  échecs pré-existants (`planning/employees`, `planning/leave`,
  `planning/swaps`, `internal/modules/ubereats`) sont strictement identiques
  avant et après, et ce lot en corrige un cinquième
  (`order_life_cycle [build failed]`) au passage.
- **Suite** : nettoyage candidat signalé (factorisation complète de
  `menu.GetAllProducts`), bug de troncature `deletion_reason_id` signalé
  (non corrigé), correctif TVA marketplace éventuel — tous explicitement
  hors périmètre de ce lot, non planifiés ici.

### RBAC lot 11, Phase 6 — runbook de déploiement production (2026-09-03)

- **Livrable** : `docs/RBAC_DEPLOIEMENT_PROD.md`. Couvre en une fois les lots
  1 à 11 (production n'a jamais reçu la moindre migration ni ligne de code
  RBAC) — pas seulement le travail de ce chantier. Étend
  `docs/RBAC_BASCULE.md` (qui s'arrêtait à la migration 099) aux migrations
  103 (lot 10) et 110 (nettoyage colonnes mortes), et au code des phases 2-4
  de ce lot.
- **Principe expansion/déploiement/contraction appliqué en 3 vagues** :
  A (schéma additif seul — 094, 095, 096, 097, 098, 099, 100, 103 —
  inoffensif car le code de production actuel ne connaît aucune de ces
  tables), B (déploiement de code + `cmd/seed_system_roles` +
  `cmd/assign_admin_role` + migration 110, dans cet ordre précis), C (retrait
  du repli historique + migration 113 — non planifiés ici, bloqués sur
  l'invariant "0 lien sans role_id en production").
- **Point clé identifié en écrivant ce runbook** : le déploiement de code de
  la vague B, à lui seul, est un no-op comportemental strict pour la
  production — `Has()` ne consulte `Permissions`/le rôle que si
  `role_id != nil`, et personne n'en a un avant que `cmd/assign_admin_role`
  ne tourne. Le vrai point de bascule est cette commande, pas le déploiement.
- **Numérotation de migrations signalée** : collision de numéro entre
  `103_permission_catalog_lot10` (RBAC) et `103_production_ready_delivery_arrival`
  (chantier temps de livraison, sans rapport) — le runbook les distingue
  explicitement par nom de fichier complet pour éviter toute ambiguïté à
  l'exécution.
- **Migration 110 (colonnes legacy déjà vivantes en production)
  explicitement isolée de la vague A** : contrairement aux autres migrations
  additives, elle touche des colonnes que le code de production lit
  aujourd'hui — placée en vague B, après confirmation du nouveau déploiement,
  jamais avant ni en même temps.
- **Suite** : chantier RBAC lot 11 terminé (phases 1 à 6). Restent, hors
  périmètre et non planifiés : le rollout réel en production (dépend d'une
  décision de planification, pas de ce chantier), et la vague C.

### RBAC lot 11, Phase 5 — le repli historique documenté, `reports.sales.read` vérifiée (2026-09-03)

- **Contexte** : phase purement documentaire, aucun changement de code — le
  brief est explicite : cette branche fait tourner la production aujourd'hui
  (migrations RBAC jamais appliquées là-bas, voir `docs/RBAC_BASCULE.md`) et
  ne doit pas être retirée par ce chantier.
- **Livrable** : `docs/RBAC_REPLI_HISTORIQUE.md` — tableau exhaustif des 18
  clés du catalogue avec/sans entrée dans `legacyPermissionFallback` (10
  avec, 8 sans), ce qui se passe pour une clé sans repli (accessible au seul
  `Rights.Admin`, jamais par défaut), et la condition exacte de retrait
  (`SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL`
  doit renvoyer 0 **en production comprise**, pas seulement en staging).
- **`reports.sales.read` vérifiée** (risque de mise en production signalé par
  le brief — c'est la clé qui garde les nouveaux endpoints analytiques) : a
  bien un repli, `CanViewReports`. Confirmé par lecture directe de
  `internal/modules/auth/permissions.go` — aucun 403 générique à craindre en
  production sur ce point.
- **Point signalé, non tranché ici (hors périmètre)** : les 5 clés du lot 10
  (`bookings.manage`, `platforms.manage`, `kiosk.manage`, `pos.analytics`,
  `seating_plan.manage`) n'ont pas de repli — avant ce lot, leurs routes
  n'avaient aucune garde RBAC (accessibles à tout compte authentifié). En
  production, où `role_id` reste NULL, l'absence de repli les a donc fait
  retomber de facto à « admin historique uniquement » — un resserrement réel,
  pas qu'une formalisation. Documenté dans `docs/RBAC_REPLI_HISTORIQUE.md`
  pour que ce soit un choix explicite si quelqu'un le remarque, pas une
  redécouverte surprise.
- **Suite** : phase 6 (runbook production, `docs/RBAC_DEPLOIEMENT_PROD.md`) —
  non commencée ici, checkpoint volontaire.

### RBAC lot 11, Phase 4 — décommissionnement de `is_admin` (`RequireAdmin` retirée, `users_rights.admin` en sursis) (2026-09-03)

- **Contexte** : suite de la phase 3. Deux routes restaient gardées par
  `middleware.RequireAdmin()` (« détient tous les droits », hors catalogue) au
  lieu d'une `permission.Key` — un chemin d'autorisation parallèle à `Has()`,
  distinct de tout ce que les phases 1-3 ont changé. Le recensement de la
  phase 1 avait déjà noté que le troisième candidat pressenti,
  `POST /users/{id}/merchant-link`, était en réalité déjà sous
  `staff.manage` : seuls deux consommateurs réels restaient.
- **Consommateurs déplacés** (`cmd/api/routes.go:625-626`) :
  `POST /users/{id}/force-reset-password` et
  `DELETE /users/{id}/merchant-link` passent de `RequireAdmin()` à
  `RequirePermission(permission.StaffManage)` — même droit que toutes les
  autres routes de gestion d'équipe juste au-dessus dans le même bloc
  (`GET/POST /users/`, `PUT /{id}/role`, etc.). Pas d'autre candidat trouvé
  (grep exhaustif `RequireAdmin\(\)` sur tout le dépôt : seuls ces deux appels
  et un snippet synthétique dans `routes_rbac_ratchet_test.go`, qui teste la
  logique générique du scanner et ne référence aucune route réelle).
- **`RequireAdmin` et `IsAdmin` retirés entièrement**, plus aucun appelant
  après le déplacement ci-dessus :
  - `middleware.RequireAdmin()` et `adminObservationKey`
    (`internal/middleware/require_permission.go`).
  - `middleware.IsAdmin()` et le fichier qui ne contenait qu'elle,
    `internal/middleware/permissions.go` (supprimé — plus rien à y garder une
    fois la fonction retirée).
  - `UserLoginRow.IsAdmin()` (`internal/modules/auth/models.go`) — l'unique
    appelant était `middleware.IsAdmin()`.
  - `HasAdminRole()` (affichage, `access.admin`/`is_admin`) **non touchée** —
    usage légitime distinct, voir le recensement de la phase 1.
- **Tests** : `internal/middleware/require_permission_test.go`,
  `TestRequirePermission_ForceResetPasswordAndMerchantLinkDelete_MovedOffRequireAdmin`
  — requête HTTP réelle à travers le routeur chi pour les deux routes
  déplacées, refusée (403) sans `staff.manage`, acceptée (200) avec, même
  gabarit que les tests `RequirePermission(permission.POSStatusManage)`
  existants. `cmd/api/routes_rbac_coverage_test.go` mis à jour (les deux
  patterns statiques attendent désormais `RequirePermission(permission.StaffManage)`
  au lieu de `RequireAdmin`).
- **Documentation tenue à jour** : `docs/RBAC_ROUTES.md` (les deux lignes +
  note d'en-tête — plus aucune route ne devrait porter `RequireAdmin`),
  `internal/middleware/rbacobserve/observer.go` (note historique sur
  l'observation `"__admin__"`, qui n'a plus de producteur).
- **`users_rights.admin` (la colonne) — en sursis, pas retirée** : à ne pas
  confondre avec le rôle admin (`roles.system_key = 'admin'`), qui portent le
  même mot pour deux choses sans rapport (c'est précisément la confusion que
  ce chantier corrige). Elle reste lue par le repli historique volontairement
  conservé en phase 5
  (`internal/modules/auth/permissions.go`, `Has()` branche `RoleID == nil` :
  `if u.Rights.Admin { return true }`), par `HasAdminRole()` (même branche,
  affichage), par `HasAccessReception()`/`CanPrintCashReport()` (affichage,
  capacités du login) et par le champ déprécié `LoginLegacyFields.Admin`. La
  migration de suppression est **préparée mais non appliquée** :
  `migrations/todo/113_drop_users_rights_admin_column.{up,down}.sql`. Elle ne
  pourra être jouée qu'une fois qu'aucun de ces lecteurs n'existe plus dans le
  code déployé — condition non remplie par ce chantier (phase 5 la garde
  intacte), voir `docs/RBAC_DEPLOIEMENT_PROD.md` (phase 6) pour l'ordre exact.
  **`docs/migration-postgres/04-schema-postgres-target.sql` volontairement
  PAS mis à jour** (contrairement à la migration 110) : cette colonne reste
  un besoin réel du code aujourd'hui, la retirer du schéma cible maintenant
  suggérerait à tort qu'elle est déjà obsolète.
- **Statut d'exécution** : `go build ./...` et
  `go build -tags postgres_integration ./...` passent. `go test
  ./internal/permission/... ./internal/modules/auth/...
  ./internal/modules/users/... ./internal/modules/roles/...
  ./internal/tasks/... ./internal/middleware/... ./cmd/api/...` passent.
- **Suite** : phase 5 (documenter précisément le repli historique — quelles
  clés en ont un, `reports.sales.read` en particulier) et phase 6 (runbook
  production) — non commencées ici, checkpoint volontaire.

### RBAC lot 11, Phase 3 — retrait du court-circuit `system_key` dans `Has()` (2026-09-03)

- **Contexte** : suite immédiate de la phase 2 ci-dessous, une fois son
  prérequis vérifié (0/30 rôle admin incomplet en staging). Le rôle
  Administrateur devient un rôle comme les autres : `UserLoginRow.Has()`
  n'accorde plus jamais un droit par la seule lecture de
  `RoleSystemKey == "admin"` — uniquement par une ligne réelle dans
  `Permissions` (elle-même chargée depuis `role_permissions`).
- **Changement** : la branche court-circuit
  (`internal/modules/auth/permissions.go`, ex-lignes 49-51) supprimée. `Has()`
  en mode rôle (`RoleID != nil`) ne fait plus qu'une seule chose : chercher la
  clé dans `Permissions`. Le repli historique (`RoleID == nil` →
  `Rights.Admin` puis `legacyPermissionFallback`) est **inchangé** — hors
  périmètre de ce lot (phase 5).
- **Tests** (`internal/modules/auth/permissions_test.go`) :
  - `TestHas_RoleMode_AdminSystemKeyDeniedWithoutExplicitGrant` — la preuve
    directe que le court-circuit est parti : un rôle `system_key=admin`
    auquel il manque une clé se voit refuser exactement cette clé (et
    seulement celle-là — les autres restent accordées).
  - `TestHas_RoleMode_AdminSystemKeyGrantedByExplicitPermissions` remplace
    l'ancien `TestHas_RoleMode_SystemKeyAdminGrantsEverything`, qui testait
    littéralement le court-circuit retiré (`Permissions: nil` suffisait) — un
    rôle admin complet accorde toujours tout, mais par appartenance
    explicite, plus par `system_key` seul.
  - Vérifié qu'aucun autre test du dépôt ne construisait un `UserLoginRow`
    avec `RoleSystemKey` en s'appuyant sur le court-circuit (grep exhaustif
    `RoleSystemKey:\s*&` — seuls `permissions_test.go` et
    `roles/service_test.go` construisent ce champ ; ce dernier ne l'utilise
    que pour des rôles `staff`, jamais `admin`).
- **`permission.SystemKeyAdmin` garde un seul rôle après ce retrait** :
  identifier *lequel* rôle marquer non supprimable / non modifiable
  (`models.ErrRoleImmutable`, garde G4). Vérifié déjà en place et suffisant
  sans changement : `UpdateRole` (rename), `ReplacePermissions` (édition des
  droits) et `ArchiveRole` (suppression) testent chacun
  `role.SystemKey != nil && *role.SystemKey == permission.SystemKeyAdmin` et
  renvoient `ErrRoleImmutable` (`internal/modules/roles/service.go:225,287,396`).
  `HasAdminRole()` (affichage `access.admin`/`is_admin`) reste également
  inchangée — c'est un usage d'affichage légitime de `system_key`, distinct de
  l'autorisation retirée ici (voir le recensement de la phase 1).
- **Statut d'exécution** : `go build ./...` passe. `go test
  ./internal/modules/auth/...` passe (12 tests, dont les 2 nouveaux/modifiés
  ci-dessus). `go test ./...` sur l'ensemble du dépôt : seuls des échecs
  **pré-existants et sans rapport** (constatés avant toute modification de ce
  lot, aucun des fichiers en cause n'étant touché) —
  `internal/modules/order_life_cycle` (échec de compilation, signature
  `customers.NewCustomersService` désynchronisée par le chantier `audit` en
  cours ailleurs dans l'arbre de travail), `internal/modules/planning/{employees,leave,swaps}`
  et `internal/modules/ubereats` (fixtures de mocks désynchronisées, sans
  rapport avec RBAC). Grep exhaustif confirmant qu'aucun autre package ne
  construit un `UserLoginRow{RoleSystemKey: ...}` ayant pu dépendre du
  court-circuit retiré.
- **Suite** : phase 4 (déplacer les deux consommateurs de
  `middleware.RequireAdmin()` — `POST /users/{id}/force-reset-password` et
  `DELETE /users/{id}/merchant-link` — vers une permission du catalogue,
  probablement `staff.manage`) — non commencée ici, checkpoint volontaire.

### RBAC lot 11, Phase 2 — invariant « rôle admin = catalogue complet », réconciliation automatisée (2026-09-03)

- **Contexte** : chantier « le rôle administrateur devient un rôle comme les
  autres » (retrait du court-circuit `RoleSystemKey == SystemKeyAdmin` dans
  `UserLoginRow.Has()`, `internal/modules/auth/permissions.go:49-51`). Ce
  court-circuit masque une dérive réelle : le recensement (phase 1 du
  chantier) a vérifié en base staging que **29 des 30 rôles admin étaient
  incomplets**, chacun manquant exactement les 5 clés du lot 10
  (`pos.analytics`, `bookings.manage`, `platforms.manage`, `kiosk.manage`,
  `seating_plan.manage`) — `go run ./cmd/seed_system_roles` n'avait jamais été
  relancé après la migration 103, alors que l'entrée du lot 10 ci-dessous le
  prescrivait. Retirer le court-circuit avant de combler ce trou aurait
  déconnecté ces 29 établissements de leurs propres droits admin. Cette entrée
  ne touche **pas** encore à `Has()` — uniquement le prérequis (phase 2 du
  chantier) : garantir l'invariant, l'automatiser, le vérifier.
- **Invariant testé** (`internal/modules/roles/postgres_integration_test.go`,
  `TestSystemAdminRolesContainFullCatalog_Postgres`) : scanne tous les rôles
  `system_key='admin'` de la base pointée par `POSTGRES_URL` et échoue si l'un
  d'eux ne porte pas l'intégralité des permissions non dépréciées du
  catalogue — même convention que `TestNoCrossTenantRoleAssignment`
  (exécutable contre dev, CI, ou un environnement réel en surchargeant
  `POSTGRES_URL`). Repose sur une nouvelle méthode,
  `Repository.FindIncompleteAdminRoles`. Un second test,
  `TestReconcileSystemRoles_BackfillsIncompleteAdminRole_Postgres`, reproduit
  la dérive constatée (supprime une permission d'un rôle admin fraîchement
  créé) et vérifie que la réconciliation la rattrape. Aucun pipeline CI
  (GitHub Actions ou autre) n'existe dans ce dépôt pour l'exécuter
  automatiquement — ces tests suivent la convention `-tags
  postgres_integration` déjà en place, seul mécanisme de gate d'intégration
  du projet à ce jour.
- **Réconciliation automatisée** : la logique de `cmd/seed_system_roles`
  (lister les établissements, `EnsureSystemRoles`, pointer
  `default_role_id`) déplacée dans `Repository.ReconcileSystemRoles`
  (`internal/modules/roles/repository.go`) — implémentation unique partagée
  par la CLI et une nouvelle tâche cron,
  `TasksManager.ReconcileSystemRolePermissions`
  (`internal/tasks/rbac.go`), enregistrée `@hourly` dans `cmd/api/tasks.go`.
  Un ajout au catalogue ne dépend plus d'une commande lancée à la main.
  Différence avec la CLI : une erreur sur un établissement n'interrompt plus
  le traitement des autres (`[]MerchantRoleReconciliation`, erreur par
  établissement) — adapté à une tâche de fond non supervisée ; la CLI, elle,
  garde un exit code non-zéro si un établissement échoue, pour qu'un run
  manuel supervisé ne passe pas inaperçu.
- **Création d'établissement déjà correcte** : `POSService.CreateMerchant`
  (`internal/modules/pos/create_service.go:52`) appelle déjà
  `EnsureSystemRoles` de façon inconditionnelle, dans la même transaction —
  vérifié, aucun changement nécessaire.
- **Staging réconcilié** (2026-09-03, `go run ./cmd/seed_system_roles` contre
  `RENDER_STAGING_DATABASE_URL`) : 30/30 établissements traités, 0 échec.
  Vérifié avant/après par requête directe et par
  `TestSystemAdminRolesContainFullCatalog_Postgres` exécuté contre staging :
  29 rôles incomplets → 0.
- **Statut d'exécution** : `go build ./...` et
  `go build -tags postgres_integration ./...` passent. `go vet -tags
  postgres_integration ./internal/modules/roles/... ./internal/tasks/...
  ./cmd/seed_system_roles/... ./cmd/api/...` ne signale que l'avertissement
  préexistant `auth/handler.go` (copie de `sync.Mutex` via
  `singleflight.Group`, sans rapport avec ce lot — voir lot 9). `go test
  ./internal/permission/... ./internal/modules/auth/...
  ./internal/modules/users/... ./internal/modules/roles/...
  ./internal/tasks/... ./cmd/api/...` passent. Tests d'intégration Postgres
  (`-tags postgres_integration`) exécutés contre staging, décrits ci-dessus.
- **Suite** : phase 3 du chantier (retrait du court-circuit dans `Has()`,
  avec test dédié « un rôle admin amputé d'une permission se voit refuser
  cette permission ») — non commencée ici, checkpoint volontaire avant d'y
  toucher.

### Droits legacy morts — nettoyage `access_delivery`/`access_waiter`/`export_reports`/`export_financials`/`export_customers` (2026-09-01)

- **Contexte** : à côté du catalogue RBAC (`permission.Key` / table `permissions`),
  le système historique de droits booléens (`UserRowRights` / colonnes
  `users_rights`) porte 17 champs. Une revue de recensement (agent de
  recherche en lecture seule, croisant l'API Go, le schéma MySQL/Postgres et
  `wello-back-office`) a montré que 5 d'entre eux ne gardent plus rien nulle
  part : ni une route API, ni une décision d'affichage côté back-office.

- **Les 5 droits retirés** :
  - `access_wrdelivery` / `access_wrwaiter` — alimentaient uniquement
    `LoginAccessResponse.Apps.Delivery/Waiter`, un mécanisme distinct du RBAC
    (pas de `permission.Key` associée ; le fallback RBAC de
    `access_wrwaiter` vers `pos.access` avait déjà disparu à la suppression
    de `pos.access` — lot 8). Confirmé mort côté `wello-back-office` (aucune
    décision de gating ne les lit) et quasi mort côté Flutter
    (`wello_resto_flutter` : un seul accesseur, déjà marqué `@Deprecated`,
    lui-même non appelé). `access_wrreception` n'est **pas** touchée : elle
    reste le fallback legacy de `pos.status.manage` et garde
    `PATCH /pos/status` pour un compte sans `role_id`.
  - `export_reports` / `export_financials` / `export_customers` — n'ont
    jamais eu de `permission.Key` dédiée (marquées « deliberately absent »
    dans `internal/modules/auth/permissions.go` depuis le lot 1/2 : lire un
    rapport et l'exporter n'ont jamais été deux droits distincts).
    Round-trippées jusqu'ici dans la réponse de login et l'éditeur de droits
    back-office (`RightsTab`), mais cet éditeur a déjà été remplacé par un
    sélecteur de rôle (`AccessTab.tsx`) — plus aucune UI ne les lit ni ne les
    coche.

- **Retiré de l'API Go** : les 5 champs de `UserRowRights`
  (`internal/modules/auth/models.go`), leurs méthodes `Has*` dérivées
  (`HasAccessDelivery`, `HasAccessWaiter`, `HasReportsExportAccess`,
  `HasFinancialsExportAccess`, `HasCustomerExportAccess`), leur lecture/
  écriture SQL (`internal/modules/auth/repository.go` ×3 requêtes,
  `internal/modules/users/{repository,admin_repository}.go`), leur
  restitution dans la réponse de login
  (`internal/modules/auth/login_response.go` :
  `LoginAccessAppsResponse.Delivery/Waiter`,
  `LoginAccessPermissionsResponse`/`LoginCapabilityActionsResponse`
  `.ExportReports/ExportFinancials/ExportCustomers`), et le DTO d'admin
  (`internal/modules/users/admin_models.go`,
  `MerchantUserPermissions`). `Capabilities.Modules.Reports/Financials/
  Customers` simplifiés en conséquence (ne dépendaient plus que du droit de
  lecture, plus du droit d'export disparu).

- **Migration** (`migrations/todo/110_drop_dead_legacy_rights_columns`) :
  `DROP COLUMN` Postgres sur `users_rights` pour les 5 colonnes, gardée par
  `to_regclass`/`IF EXISTS` (même convention que la migration 104).
  `docs/migration-postgres/04-schema-postgres-target.sql` mis à jour en
  miroir. Pas de traduction MySQL séparée : le schéma cible de ce dépôt est
  Postgres (voir les migrations 094+ sous `migrations/todo/`) — MySQL est
  l'état de départ historique, pas une cible à maintenir en parallèle.

- **Retiré de `wello-back-office`** : `MerchantUserPermissions`
  (`src/types/adminUsers.ts`), `AuthCapabilities.apps`/`.actions` et le
  fallback `allow_delivery_account`/`allow_waiter_account`
  (`src/types/auth.ts`), les fixtures (`src/services/mocks/teamMocks.ts`,
  `src/services/authService.ts`) et le payload figé de liaison d'un
  utilisateur existant (`src/components/team/CreateMemberSheet.tsx`).
  `access_reception` conservée partout (toujours vivante côté API).
  `npx tsc --noEmit` et `npx eslint` passent sans erreur sur ce dépôt après
  coup.

- **Hors périmètre, non tranché ici** : `access_wrreception` /
  `access_wrdelivery` / `access_wrwaiter` alimentent aussi
  `LoginAccessResponse.Apps`, un mécanisme distinct du RBAC potentiellement
  consommé par d'autres apps clientes (Kiosk, ScanNOrder) non auditées dans
  cette passe — seul `wello_resto_flutter` a été vérifié pour `access_wrdelivery`/
  `access_wrwaiter` (mort ou quasi mort, voir ci-dessus).

### RBAC lot 10 — 5 nouvelles clés (bookings/platforms/kiosk/analytics/plan de salle) + backfill des descriptions (2026-08-28)

- **Contexte** : plusieurs surfaces back-office restaient accessibles à tout
  utilisateur authentifié sans droit RBAC dédié — paramètres de réservation,
  onglet « Canaux et Plateformes », onglet « Kiosk », page « Analyse », page
  « Plan de salle ». Migration `migrations/todo/103_permission_catalog_lot10.up.sql`
  ajoute 5 clés : `bookings.manage`, `platforms.manage`, `kiosk.manage`,
  `pos.analytics`, `seating_plan.manage`. `internal/permission/keys_gen.go`
  mis à jour en miroir (13 → 18 clés).
- **Principe de garde appliqué** (repris du lot 8) : CONFIGURATION gardée,
  CONSULTATION/SAISIE courante laissée libre.
  - `bookings.manage` : uniquement `/bookings/settings*` (PUT settings,
    CRUD des `duration-rules`, PUT hours). La liste/gestion courante des
    réservations (`POST /create`, accept/deny/cancel/seat/…) n'est **pas**
    concernée — hors périmètre de la demande, qui ne visait que les
    paramètres.
  - `platforms.manage` : toutes les routes de configuration sous
    `/integrations` (PATCH uber-eats/deliveroo/scannorder, onboarding
    scannorder, logo/banner, toggles globaux, liens Stripe Connect/branding).
    `GET /integrations/stripe/balance` reste gardé par
    `reports.financial.read` seul (lot 8) — pas de double garde.
  - `kiosk.manage` : les mutations de `/pos/settings/kiosk` (codes
    d'enrôlement, devices, réglages/logo/idle-media). Les deux routes
    admin-pin restent gardées par `settings.manage` seul (exception
    documentée de longue date, `docs/KIOSK_DECISIONS.md`, non touchée). Les
    `GET` restent libres.
  - `pos.analytics` : ancrage unique `GET /stats/upsell` — seule route
    "analytics" qui n'était couverte par aucun droit `reports.*` existant.
    La page « Analyse » du front peut afficher d'autres widgets déjà
    couverts par `reports.sales.read`/`reports.financial.read` ; seul
    `pos.analytics` gate la page dans son ensemble côté front.
  - `seating_plan.manage` : tout `/floors*` (CRUD floors/obstacles/areas) et
    les mutations de tables sous `/locations` (`POST .../tables`, PATCH/DELETE
    `/tables/{id}`). `GET /locations` reste libre — utilisé ailleurs (prise
    de commande, `AssignBookingLocations`), pas seulement par la page « Plan
    de salle ».
- **`sort_order`** : `pos.analytics` = 55 (rejoint le groupe `pos.*` existant,
  entre `pos.cash_drawer.open`=50 et `catalog.manage`=60) ; les 4 autres sont
  de nouveaux domaines, ajoutés après `settings.manage`=140 (150/160/170/180).
- **Backfill des `description`** : les 13 clés existantes avaient toutes une
  `description` vide (`095` l'omettait délibérément — "content is a
  product-copy task"). Le back-office affiche maintenant systématiquement ce
  champ sous chaque droit (retravail `PermissionsEditor.tsx` de la même
  session côté front) ; laissé vide, ça rendait une ligne blanche sous
  chaque droit. La migration 103 fait l'`UPDATE` des 13 existantes en même
  temps qu'elle insère les 5 nouvelles avec une description dès le départ.
- **Ratchet RBAC** (`cmd/api/routes_rbac_ratchet_test.go`) : le nombre de
  routes mutatives non gardées passe de 212 à 175 — `unguardedMutativeRouteCeiling`
  abaissé en conséquence.
- **Backfill des rôles existants** : cette migration ajoute des clés au
  catalogue mais ne touche `role_permissions` d'aucun établissement — à
  lancer juste après déploiement : `go run ./cmd/seed_system_roles` (même
  précédent que pour `097_permission_pos_status_manage`), pour que le rôle
  « Administrateur » de chaque établissement récupère les 5 nouvelles clés.
- **Statut d'exécution** : `go build ./...` et
  `go test ./internal/permission/... ./cmd/api/... -run TestAllMatchesMigrationCatalog|TestNoDuplicateKeys|TestRBACPermissionCoverage|TestRBACRatchet`
  passent. Tests d'intégration Postgres non relancés (pas d'accès
  Postgres/Redis locaux dans cette session — même limite que les lots
  précédents).

### RBAC lot 9 — clés à points dans /login, admin dérivé du rôle, rôle exposé sur GET /users/{id} (2026-08-27)

- **Contexte** : lot 9 = écrans d'administration des rôles côté back-office
  (dépôt `wello-back-office`). Deux prémisses du brief front se sont révélées
  fausses à la lecture du code, corrigées ici côté API avant que le front ne
  s'appuie dessus.

- **`permissions: string[]` ajouté à la réponse de login**, en sibling de
  `access` (pas dedans — `access.permissions` garde sa forme et ses noms
  historiques, d'autres clients en dépendent).
  `internal/modules/auth/login_response.go` (nouveau champ) +
  `service.go` (`buildLoginResponse`, `Permissions: user.Permissions`). Donnée
  déjà calculée par `attachRolePermissions`/`loadRolePermissions`
  (`repository.go`, RBAC lot 2) — rien de recalculé. Un seul site de
  construction réel existe (`buildLoginResponse`, appelé uniquement par
  `AuthService.Login`) : le login par mot de passe, le login par PIN
  (`AuthenticatePIN` délègue à `Login`) et la bascule d'établissement
  (`switchMerchant`/`loginWithToken` côté back-office, `POST /auth/login`
  avec un autre token porteur) passent tous par ce même chemin. `LoginOld`
  est du code mort (entièrement commenté). La restauration de session est
  100% côté client (localStorage, aucun appel réseau) — aucun chemin API à
  couvrir de ce côté.
- **Garde-fou `permission.FilterValid`** (`internal/permission/filter.go`) :
  ne garde que les clés présentes dans `permission.All`, appelé dans
  `attachRolePermissions` avant d'écrire `data.Permissions`. La contrainte FK
  de `role_permissions.permission_key` devrait déjà garantir cet invariant
  (cf. entrée du lot 8 ci-dessous) ; ce filtre le rend explicite et testable
  plutôt que de reposer uniquement sur la contrainte DB. Tests :
  `internal/permission/filter_test.go`,
  `internal/modules/auth/login_response_test.go`
  (`TestBuildLoginResponse_PermissionsPassthrough` — le pendant, côté sortie,
  de `TestRBACPermissionCoverage`/`keys_gen_test.go`).

- **Bug trouvé en cours de route, corrigé le même jour** : `access.admin`
  (réponse de login) et `is_admin` (`GET /me/permissions`) lisaient tous les
  deux directement `user.Rights.Admin` — la colonne booléenne historique,
  jamais le rôle. Or `Has()` (l'autorisation réelle, utilisée par
  `RequirePermission`) ignore déjà `Rights.Admin` dès que `role_id` est
  renseigné, même s'il contredit le rôle (`permissions.go:43-64`, testé). Les
  deux champs d'affichage étaient donc en décalage avec l'autorisation
  réelle — sans conséquence pour l'API elle-même (les routes catalogue
  restent correctement gardées), mais un risque concret pour ce lot : un
  compte non-admin avec `Rights.Admin` resté à `true` (signalé comme
  fréquent en production) aurait affiché `access.admin = true`, et
  `usePermissions().has()` côté front court-circuite sur ce drapeau — aucun
  menu n'aurait jamais été masqué, quel que soit le rôle réellement assigné.
  **Correctif** : nouvelle méthode `UserLoginRow.HasAdminRole()`
  (`internal/modules/auth/permissions.go`), qui reprend exactement la
  branche admin de `Has()` (role_id renseigné → `RoleSystemKey == "admin"` ;
  sinon repli sur `Rights.Admin`). Utilisée par `buildLoginResponse` pour
  `access.admin` et par `roles.Service.MyPermissions` pour `is_admin`
  (remplace le `OR` avec `Rights.Admin` qui y subsistait). **Distincte
  d'`IsAdmin()`** (`models.go`), qui reste `Rights.Admin` tel quel et continue
  de servir `middleware.RequireAdmin()` — une décision d'autorisation
  différente (« détient tous les droits », hors catalogue), non touchée ici,
  hors périmètre du lot 9. Tests : 4 cas ajoutés à
  `internal/modules/auth/permissions_test.go`.

- **`GET /users/{id}` expose désormais `role_id`/`role`** (nécessaire pour le
  sélecteur de rôle du nouvel onglet « Accès » côté back-office).
  `internal/modules/users/admin_repository.go` :
  `GetMerchantUserByID` a désormais sa propre requête (LEFT JOIN `roles`) et
  son propre scan (`scanMerchantUserDetail`), plutôt que de faire grossir la
  requête/scan partagés avec `ListMerchantUsers`
  (`scanMerchantUserListItem`) — la liste n'a pas besoin de ces colonnes.
  `MerchantUserDetail` (`admin_models.go`) gagne `RoleID *string` et
  `Role *RoleRef` (nil si l'utilisateur n'a pas encore de `role_id`, monde
  pré-lot-4). Handler inchangé : `models.SendJSON` sérialise la struct
  directement, sans liste blanche de champs — confirmé en lisant
  `admin_handler.go`. Fixture de test partagée
  (`merchantUserDetailRows`, `admin_service_test.go`) mise à jour pour les 3
  colonnes supplémentaires (nulles — aucun test existant n'exerce le chemin
  rôle).

- **Statut d'exécution** : `go build ./cmd/api/...` (et un `go build` scopé
  aux paquets touchés — l'environnement de build a rencontré un disque C:
  plein pendant cette session, contournement en limitant le scope plutôt
  qu'un `./...` complet) ; `go test ./internal/permission/...
  ./internal/modules/auth/... ./internal/modules/users/...
  ./internal/modules/roles/...` passent, y compris après correction de la
  fixture `merchantUserDetailRows`. `go vet` sur les mêmes paquets ne
  signale que deux avertissements préexistants dans `auth/handler.go` (copie
  de `sync.Mutex` via `singleflight.Group`), non liés à ce lot. Tests
  d'intégration Postgres non relancés (pas d'accès Postgres/Redis locaux
  dans cette session — même limite que les lots précédents).

### RBAC lot 8 — catalogue 15 → 13, trois gardes mal posées retirées (2026-08-27)

- **Contexte** : RBAC lot 7 (audit, `docs/RBAC_CLIENTS.md`) avait trouvé trois
  gardes de sur-dosage (un droit `*.manage` posé sur une CONSULTATION ou une
  SAISIE courante plutôt que sur une CONFIGURATION/CORRECTION) et huit droits
  du catalogue qui ne gardaient aucune route. Ce lot traite les deux : retire
  les trois gardes, relie six des huit droits orphelins à une route, et
  supprime les deux restants du catalogue. Un des trois retraits a lui-même
  fait naître un neuvième orphelin en cours de route (`haccp.manage`, voir
  ci-dessous) — également résolu le même jour.

- **`/haccp/traceability` — garde `haccp.manage` retirée (POST, GET, GET
  `/{id}`).** Motif d'origine de la garde (posée le 2026-07-23, commit
  `0b4509f` « ready for staging », message sans contexte) : **aucune trace
  écrite n'en subsistait dans le dépôt** — ni `docs/decisions.md` (qui a une
  entrée à cette date, mais sur un tout autre sujet : un trou de schéma
  Postgres pour la même fonctionnalité), ni aucun autre document, ni le
  message de commit lui-même. Confirmé par l'auteur de la décision dans cette
  session : **le raisonnement était le même que celui retiré ci-dessous pour
  `POST /customers/`** — « ça écrit des données », donc ça mérite une garde.
  C'est exactement le raisonnement que le principe directeur du lot 7 invalide
  (écrire une donnée ne rend pas une action CONFIGURATION — relever une
  température écrit aussi une donnée, et n'est pas gardé). La traçabilité
  HACCP (réception de marchandise tracée, photo + commentaire) est une
  obligation légale quotidienne, saisie par n'importe quel employé de cuisine
  via l'app Flutter — exactement comme le relevé de température ou le log de
  nettoyage juste à côté dans le même module, tous deux libres. Lecture ET
  écriture passent donc libres, alignées sur le reste de `/haccp`.
  **Conséquence, tranchée le jour même** : retirer cette garde a laissé
  `haccp.manage` sans aucune route (orphelin au sens du lot 7/8), attrapé par
  `TestRBACPermissionCoverage`. Décision (même journée, 2026-08-27) : `PUT
  /haccp/settings` (paramétrer les seuils et réglages du module — CONFIGURATION,
  à l'inverse de la traçabilité qui est de la SAISIE) porte maintenant
  `haccp.manage`. Les autres candidats CONFIGURATION identifiés par l'audit
  (créer/éditer les zones et surfaces de nettoyage, les zones de température)
  restent libres, non traités par ce lot. **Suivi explicitement différé à un
  lot futur, non implémenté ici** : masquer la section HACCP du menu
  back-office pour un compte sans `haccp.manage`, symétriquement à la garde
  API — aujourd'hui rien ne cache ce menu, un compte sans le droit verrait
  l'écran de paramétrage puis un 403 à l'enregistrement.

- **`POST /customers/` (création unitaire) — garde `customers.manage`
  retirée.** Créer une fiche client est une SAISIE courante (un serveur
  inscrivant un client au programme de fidélité en salle en a besoin au
  quotidien), pas une CONFIGURATION — `customers.manage` reste sur les 3
  routes d'import en masse, qui écrivent potentiellement tout le fichier
  client d'un coup. `GET /customers/list` et `GET /customers/search` restent
  libres, comme avant ce lot (confirmé, aucun changement).

- **`pos.access` et `pos.discount.apply` supprimés du catalogue** (migration
  `100_deprecate_pos_access_and_discount_apply`). Ni l'un ni l'autre ne
  gardait de route, et aucun remplacement n'est prévu : encaisser et
  appliquer une remise restent, comme ils l'ont toujours été, des gestes
  libres pour tout compte authentifié — ce ne sont pas des actions qu'un
  restaurateur voudrait réellement restreindre à certains employés. La
  migration supprime d'abord les lignes `role_permissions` référençant ces
  deux clés sur tous les rôles de tous les établissements (contrainte FK,
  même raisonnement que le down de la migration 095), puis les deux lignes du
  catalogue `permissions`. `internal/permission/keys_gen.go` régénéré (15 →
  13 constantes) ; `legacyPermissionFallback`
  (`internal/modules/auth/permissions.go`) perd l'entrée `pos.access` ->
  `AccessWaiter`.

- **Rôle système « Employé polyvalent » (`system_key = 'staff'`) : ne porte
  plus aucun droit par défaut.** C'était le seul rôle du catalogue à porter
  `pos.access` et `pos.discount.apply` par défaut
  (`internal/modules/roles/repository.go`, `systemRolePermissions[SystemKeyStaff]`,
  désormais `{}`) — les deux droits supprimés ci-dessus. C'est le résultat
  attendu de cette suppression, pas une régression à corriger après coup :
  tout ce qu'un employé polyvalent fait au quotidien (encaisser, appliquer
  une remise, relever une température, tracer une réception, prendre une
  commande...) reste et est resté intégralement libre côté routes — les 13
  droits qui restent au catalogue gardent tous des gestes d'encadrement
  (correction : rouvrir un ticket, rembourser ; configuration : gérer le
  menu, le planning, les stocks ; rapport : consulter les ventes ou les
  finances) qu'un employé polyvalent n'exerce pas par définition du rôle.
  Aucun droit n'a été réattribué arbitrairement pour « remplir » le rôle —
  un rôle vide qui garde son nom et sa place est l'état correct ici, pas un
  trou à combler. `staff` reste entièrement client-éditable : un
  établissement qui veut lui donner des droits d'encadrement le fait via
  `PUT /roles/{id}/permissions`, comme pour n'importe quel rôle personnalisé.

- **Trace laissée pour la prochaine fois** : le motif de la garde HACCP de
  juillet n'existait nulle part par écrit — reconstitué ici en croisant le
  commit qui l'a posée, `docs/decisions.md`, `docs/migration-postgres/56-*`
  et `docs/PERMISSIONS_MIDDLEWARE_GUIDE.md`, pour un motif qui tient en une
  phrase. Cette entrée existe pour que personne n'ait à refaire ce travail
  pour les décisions d'aujourd'hui.

### Reservation (site public) — double conversion de fuseau à la création, résa décalée de -2h (2026-08-13)

- **Symptôme rapporté** : sur le site de réservation public, sélectionner
  un créneau à 19h (heure marchand, Europe/Paris/CEST) stockait
  `booking_date_from` = 17h+02:00, soit 15h UTC au lieu des 17h UTC
  attendus (19h CEST) — un décalage exact d'un offset marchand (-2h) par
  rapport à la valeur correcte. Le POS affichait donc 17h, correctement,
  vu que le correctif précédent (cf. entrée du jour "heure décalée dans la
  liste des résas") lit fidèlement ce qui est en base — la donnée
  elle-même était fausse à l'écriture, pas seulement mal affichée.
- **Root cause : double conversion locale→UTC dans le flux public
  `POST /rsv/{slug}/booking/create`** (`internal/modules/reservation/`) :
  1. `service.go:193-194` (`CreateReservation`) parse correctement
     `"19:00:00"` avec `time.ParseInLocation(..., merchant.Timezone)` →
     19:00 CEST = 17:00 UTC. Correct.
  2. `service.go:218-219` reconvertit ce temps en chaîne UTC naïve
     (`"17:00:00"`, sans marqueur de fuseau — indiscernable d'une heure
     locale dans le format `"2006-01-02 15:04:05"`) et **réécrit
     `req.Booking.StartDate`/`EndDate` avec cette valeur déjà-UTC**.
  3. `repository.go:434-443` (`CreateBookingTransaction`) recevait cette
     chaîne déjà-UTC mais la reparsait **avec le fuseau marchand**
     (`loadMerchantLocation` + `ParseInLocation(..., loc)`), la traitant à
     tort comme de l'heure locale une deuxième fois → 17:00 CEST = 15:00
     UTC stocké.
  - `bookingcore.CreateBooking` (partagé avec le flux staff) n'est pas en
    cause : il fait fidèlement le seul `.UTC()` nécessaire sur la valeur
    qu'on lui passe — le problème est amont, dans la valeur déjà faussée
    reçue.
- **Écosystème audité pour écarter les autres suspects** :
  - Site public (`luxury-table-booking`) : confirmé innocent — le payload
    `start_date` est une simple concaténation de chaînes
    (`` `${date} ${time}:00` ``, `src/lib/api.ts`), aucun objet `Date`,
    aucune conversion, aucune lib de fuseau (`dayjs`/`luxon`/`date-fns-tz`)
    sur ce chemin, vérifié source + bundle buildé.
  - Flux staff (`internal/modules/bookings`) : non affecté — son
    `service.go` ne pré-convertit jamais en UTC, le repository fait
    l'unique `ParseInLocation` avec le fuseau marchand.
  - `UpdateReservation` (reschedule public, `service.go:311-363`) : même
    pré-conversion UTC en `service.go:350-351`, mais **pas de bug** —
    `repository.go UpdateBooking` (ligne 405-416) ne reparse jamais,
    bind directement la chaîne UTC déjà prête dans la requête SQL. C'est
    ce contraste (`UpdateBooking` correct vs `CreateBookingTransaction`
    buggé) qui a confirmé où était l'unique conversion de trop.
  - Idempotence (`tryIdempotentReplay`/`saveIdempotencyResult`/
    `clearPendingIdempotency`) vérifiée : clé sur `(slug, idempotencyKey)`
    uniquement, aucune dépendance au format de date — sans impact du
    correctif.
  - `FindExistingActiveBookingWarning` (`service.go:225`,
    `repository.go:288-314`) vérifiée : compare `booking_date_from = ?`
    directement contre la colonne (vraie UTC), donc a bien besoin de la
    chaîne déjà-UTC produite par `service.go:219` — confirme que la
    pré-conversion UTC en `service.go` doit rester ; le correctif ne
    devait toucher que le second parse en trop côté repository.
- **Correctif appliqué** (`internal/modules/reservation/repository.go`,
  `CreateBookingTransaction`) : les deux `ParseInLocation` reparsent
  désormais avec `time.UTC` au lieu du fuseau marchand (la chaîne reçue
  est déjà UTC à ce stade — un simple parse, pas une conversion). Le
  helper `loadMerchantLocation` de ce fichier, devenu sans appelant,
  supprimé.
- **Statut d'exécution** : `go build ./...` et
  `go vet ./internal/modules/reservation/...` (y compris sous
  `-tags postgres_integration`, pour couvrir `postgres_integration_test.go`)
  passent. **Dette de test notée** : `internal/modules/reservation` n'a
  aucun test unitaire (`go test` → `[no test files]`), et son seul test
  d'intégration (`postgres_integration_test.go`) seed les données
  directement en SQL sans passer par `CreateReservation`/
  `CreateBookingTransaction` — il n'exerçait donc pas ce chemin avant le
  bug et ne le couvre toujours pas après le correctif. Une connexion
  Postgres locale de dev (`POSTGRES_URL`, cf. doc
  `internal/database/dbx/pgtest/pgtest.go`) serait nécessaire pour ajouter
  et exécuter un test de bout en bout ; non fait cette session (seule
  `RENDER_STAGING_DATABASE_URL`, une base staging partagée, était
  disponible — écarté pour ne pas faire tourner un test mutateur dessus
  sans validation préalable).

### Bookings — lien de gestion dans le SMS, au même titre que l'email (2026-08-13)

- **Demande** : le SMS envoyé au client ne contenait pas de lien vers la
  page de gestion de sa réservation, contrairement à l'email qui a déjà un
  bouton "Gérer ma réservation" (`{{if .ManagementLink}}` dans les
  templates `booking_confirmation.html`, `booking_reminder.html`,
  `booking_modification.html`, `booking_reconfirmation.html`).
- **Où** : tout le montage email/SMS des réservations est centralisé dans
  `internal/modules/bookingcomm/service.go` (`BookingMessage`, un seul
  point d'envoi par type de message, découplé des modules métier `bookings`/
  `reservation` pour éviter les cycles d'import). Le lien existait déjà,
  construit par `BookingMessage.managementLink(baseURL)` (`{baseURL}/restaurant/{slug}/booking/{bookingNumber}`,
  `baseURL` = `PUBLIC_RESERVATION_BASE_URL`) mais n'était consommé que par
  `emailData()` pour l'email — jamais passé aux `fmt.Sprintf` des corps SMS.
  Tous les call sites (`bookings/communication.go`, `bookings/reminders.go`,
  `bookings/service.go`, `reservation/service.go`) peuplent déjà
  `MerchantSlug`+`BookingNumber` sur le `BookingMessage`, donc aucun
  plumbing supplémentaire n'était nécessaire.
- **Correctif** : nouvelle méthode `(*Service) smsWithManagementLink(m, text)`
  qui ajoute le lien (même lien que l'email) en suffixe du corps SMS,
  séparé par un espace, et renvoie le texte inchangé si le lien est
  indisponible (slug/booking_number manquant ou `baseURL` non configuré) —
  évite l'espace final orphelin. Branché sur `SendConfirmation`,
  `SendReminder`, `SendModification`, `SendReconfirmation`. **Pas** branché
  sur `SendCancellation`, à l'image de l'email : `booking_cancellation.html`
  n'a pas de bloc `ManagementLink`, rien à gérer une fois la résa annulée.
  Liste d'attente (`SendWaitlistAvailable`/`WaitlistMessage`) non concernée :
  pas de réservation existante à gérer, pas de `BookingNumber`/`MerchantSlug`
  dans cette struct.
- **Point de vigilance signalé, non traité** : aucune limite de longueur/
  encodage GSM-7 n'existe dans le code SMS (`brevo_sms/service.go`) — les
  corps étaient déjà proches d'un segment de 160 caractères pour certains
  marchands/numéros de résa ; l'ajout de l'URL (~50-70 caractères selon le
  slug) fera probablement basculer une partie des envois en multi-segments
  (facturation Brevo par segment). Aucune action prise sur ce point, à
  arbitrer séparément si le volume/coût SMS devient un sujet.
- **Statut d'exécution** : `go build ./...`, `go vet ./internal/modules/bookingcomm/...`
  et `go test ./internal/modules/bookingcomm/... ./internal/modules/bookings/...`
  passent. Tests ajoutés (`service_test.go`) vérifiant la présence du lien
  dans le SMS pour confirmation/rappel/modification/reconfirmation, son
  absence pour l'annulation, et l'absence de lien + absence d'espace final
  quand `PUBLIC_RESERVATION_BASE_URL` n'est pas configuré. Pas de vérification
  d'envoi réel (Brevo) dans cette session.

### Bookings — heure décalée (-2h) dans la liste des résas tablette POS (2026-08-13)

- **Symptôme rapporté** : une résa créée pour 18h heure française (stockée
  correctement en base — `booking_date_from` = l'instant UTC équivalent,
  vérifié via inspection directe) s'affichait à 16h dans le carnet de
  réservations de la tablette POS (Flutter).
- **Root cause identifiée** : régression de la migration `056_bookings_dates_utc`
  (bascule du stockage `booking_date_from`/`booking_date_to` en UTC).
  `ListBookingsBackOffice` (`internal/modules/bookings/repository.go`,
  utilisé par `GET /bookings`, l'endpoint carnet de résas de la tablette)
  formatait encore la date via `bkgDateTimeFmt` (`to_char`/`DATE_FORMAT`)
  **sans conversion vers le fuseau du marchand** — correct avant la 056
  quand la colonne stockait déjà l'heure locale marchand en naïf, faux
  depuis puisque `to_char` restitue l'heure "wall-clock" du fuseau de
  session Postgres (UTC), renvoyée en chaîne brute sans offset. Le client
  Flutter (`booking_date_utils.dart`, fonction `bookingDateTimeFromUtcString`
  — mal nommée, elle ne faisait aucune conversion UTC→local) parsait cette
  chaîne en supposant qu'elle était déjà en heure locale marchand (c'était
  vrai avant la 056, plus depuis). D'où le décalage exact de l'offset du
  marchand (+2h l'été).
- **Endpoint détail (`GetBooking`) non affecté** : celui-ci renvoyait déjà un
  timestamp Unix UTC (`bookings_fetcher.go`), correctement reconverti côté
  client par `bookingDateTimeFromUnixSeconds`
  (`DateTime.fromMillisecondsSinceEpoch` → heure locale de l'appareil). Ce
  pattern déjà existant a servi de référence pour le correctif.
- **Autres usages de `bkgDateTimeFmt("b.booking_date_from")` audités et
  écartés** : `ListPendingBookingsToExpire` / `ListBookingsForReminder`
  (SMS/email de rappel) utilisent aussi ce format brut UTC, mais
  `BookingContact.StartDate` est explicitement documenté `// UTC` et
  reconverti côté Go dans `reminders.go` via `time.LoadLocation(b.Timezone)`
  avant formatage du message — pattern déjà correct, non touché.
- **Correctif appliqué (serveur + client, décision : aligner sur le pattern
  Unix déjà en place plutôt que patcher le format string)** :
  - Serveur : `ListBookingsBackOffice` sélectionne désormais la colonne
    brute `b.booking_date_from` (au lieu de `bkgDateTimeFmt(...)`), scannée
    en `sql.NullTime` puis convertie en Unix UTC (`helpers.NullTimePtr(...).UTC().Unix()`,
    même pattern que `bookings_fetcher.go`). `BookingListItem.BookingDateFrom`
    passe de `string` à `int64` (`internal/modules/bookings/models.go`).
  - Client : `BookingListItemDto.dateFrom` bascule sur
    `bookingDateTimeFromUnixSeconds` (comme `BookingDto`) ; l'ancienne
    fonction `bookingDateTimeFromUtcString`, plus utilisée nulle part,
    supprimée (`booking_date_utils.dart`).
- **Non traité ici, signalé mais hors périmètre demandé** : les filtres
  `date_from`/`date_to` de `GET /bookings` (jour affiché sur la tablette)
  sont envoyés par le client comme bornes de journée en heure locale
  naïve (`bookings_api.dart`, `_formatDateTime` sur un `DateTime` local)
  et comparés tels quels à la colonne UTC côté SQL
  (`ListBookingsBackOffice`, `repository.go`) — même classe de bug que
  celui corrigé ici, mais côté filtrage plutôt qu'affichage. Impact limité
  aux résas proches de minuit (le reste de la journée retombe dans la même
  fenêtre malgré le décalage). À traiter séparément si confirmé.
- **Statut d'exécution** : `go build ./...`, `go vet ./internal/modules/bookings/...`
  et `go test ./internal/modules/bookings/...` passent (tests unitaires ;
  la suite `postgres_integration_test.go` n'a pas été relancée, nécessite
  une base Postgres vivante). Côté Flutter, `flutter analyze` passe sur les
  deux fichiers modifiés ; pas de test automatisé existant sur ce chemin,
  vérification manuelle sur device non faite dans cette session.

### Rapport comptable "réel" — deux correctifs d'audit + dette de test notée (2026-08-11)

- **Asymétrie `COALESCE`/`NULLIF` corrigée** : la branche
  `cash_registers_items` de `GetRealPaymentsData`
  (`internal/modules/pos/accounting/repository.go`) ne gérait qu'un
  `labels.label` `NULL` (`COALESCE(l.label, cri.mop)`), pas une ligne
  `labels` existante mais au libellé vide (`''`) — contrairement à la
  branche `cash_registers_custom_items`, déjà en
  `COALESCE(NULLIF(l.label, ''), ...)`. Un libellé `''` produisait une
  ligne à blanc dans le rapport (ou pire, une ligne invisible : `''` fait
  disparaître le montant du regroupement par libellé attendu) au lieu du
  repli sur le code brut. Alignée sur le même pattern des deux côtés.
  Confirmé par un cas de test dédié (`labels.label=''` pour un MOP connu) :
  échoue sans le correctif, passe avec.
- **Détail du `log.Warn` de dérive enrichi** : `GetTrustedEnclosedRegisterIDs`
  loggait seulement `cash_register_id`/`merchant_id` en cas d'écart. Ajoute
  le(s) MOP en dérive avec montant figé (`cash_registers_items`) et montant
  live recalculé, pour que le support puisse diagnostiquer un événement de
  dérive sans avoir à re-dériver l'écart manuellement en base.
- **Dette de test notée, non traitée ici** : ce `log.Warn` n'est vérifié
  que par lecture de code, pas par une assertion automatisée — les
  binaires de test `postgres_integration` n'appellent jamais
  `zap.ReplaceGlobals` (contrairement à `cmd/api/main.go`), donc
  `logger.FromContext` retombe sur le logger zap global no-op et rien
  n'est capturable depuis un test tel quel. Pour fermer ce trou
  proprement : injecter un logger observer via `logger.WithLogger(ctx,
  ...)` dans le contexte de test (pattern à créer, n'existe pas encore
  dans ce dépôt) plutôt que de compter sur `context.Background()` nu.

### `TestPOSAccountingReports_Postgres` — échec pré-existant, sans rapport avec ce dépôt (2026-08-11)

- Le sous-test `GetTVAData = (..., want 1 ligne)` de
  `TestPOSAccountingReports_Postgres`
  (`internal/modules/pos/accounting/postgres_integration_test.go`) échoue
  contre le Postgres de dev actuel : une vraie catégorie `tva_id=-1`
  ("TVA Delivery fees 20%", `tva_desc` ≠ `'itest'`) existe déjà dans la base
  partagée, ce que l'assertion (écrite avant l'existence de cette ligne)
  n'anticipait pas — `GetTVAData` retourne une ligne "frais de livraison" à
  TTC=0 pour toute commande sans frais de livraison dès que cette catégorie
  sentinelle existe globalement (`repository.go`, jointure
  `tva_fees.tva_id = '-1'` sans filtre `delivery_fees > 0`).
- **Confirmé indépendant du chantier "réel des caisses closes"** (entrée
  suivante) : reproduit à l'identique en stashant les 3 commits de ce
  chantier et en relançant le test sur le code d'origine, contre la même
  base. Aucun fichier de ce chantier ne touche `GetTVAData`,
  `tva_categories`, ni les fixtures de ce test.
- **Non corrigé ici** (hors périmètre) : soit scoper l'assertion aux lignes
  du test (même pattern que `TestGetCashRegisterReport_Postgres`, qui filtre
  déjà ses assertions par clé plutôt que par nombre total de lignes, pour
  cette même raison de table de référence globale partagée), soit filtrer
  `delivery_fees > 0` dans la branche frais de livraison de `GetTVAData` —
  à trancher et traiter dans un ticket dédié.

### Rapport comptable — section Encaissements sur le réel des caisses closes (2026-08-11)

- **Contexte** : un bug (`orders.price=0` alors que `orderitems` avait les
  bons prix) a fait diverger le total TVA et le total Encaissements du
  rapport comptable mensuel d'un merchant, révélant que la section
  Encaissements (`AccountingRepository.GetPaymentsData`,
  `internal/modules/pos/accounting/repository.go`) sommait le théorique en
  direct (table `payments`), sans lien avec ce que le restaurateur déclare
  réellement à la clôture de caisse. Décision produit (sur le modèle
  Zelty) : la section Encaissements du PDF affiche désormais le **réel
  uniquement** — `cash_registers_items` (figé à la clôture) +
  `cash_registers_custom_items` (ajustements manuels du caissier) — pour
  les registres `enclosed=true` dans la période, **sans repli théorique
  mélangé dans ce rapport**. Un merchant/mois sans registre correctement
  clôturé affiche une table vide avec un message explicite plutôt qu'un
  tableau théorique.
- **`GetPaymentsData` (théorique) non modifié** : reste utilisé tel quel
  par `internal/modules/pos/reports` (page back-office séparée,
  `/pos/reports/tva` et `/pos/reports/payments`, déjà l'extraction
  théorique jour par jour) — aucune régression possible puisqu'aucun
  fichier de ce module n'est touché.
- **Nouvelles méthodes** `GetTrustedEnclosedRegisterIDs` et
  `GetRealPaymentsData` (`accounting/repository.go`) :
  - Garde-fou anti-dérive : `cash_registers_items` est un instantané figé
    à la clôture, jamais recalculé. Avant de faire confiance à un
    registre `enclosed`, son instantané est comparé à un recalcul live
    des mêmes paiements (même filtre que `cashRegisterReportMOPSQL`,
    `internal/modules/cash_registers/repository.go`). Le moindre écart
    par `(cash_register_id, mop)` — ex. un paiement corrigé sur une
    commande après que son registre a été enclosed, exactement le cas qui
    a motivé ce chantier — écarte le **registre entier** du réel pour ce
    rapport (pas de repli partiel mélangé).
  - Exclusion de canal (`accountingExcludedChannelMOPs` :
    `STRIPE`/`UBER_EATS`/`DELIVEROO`) : `cash_registers_items` est déjà
    agrégé par `(cash_register_id, mop)` à la clôture et ne porte donc pas
    nativement le filtre `brand`/`created_by` du théorique — Uber Eats et
    Deliveroo ont leur propre gestion TVA à venir, `STRIPE` est
    exclusivement ScanNOrder (confirmé par recherche exhaustive dans le
    repo).
  - `LEFT JOIN` volontaire vers `labels` (au lieu de `INNER JOIN` comme le
    théorique) : un code MOP non libellé ne doit jamais faire disparaître
    silencieusement un montant du total censé être le plus fiable —
    affiché sous son code brut à défaut de libellé. Un custom item à
    texte libre sans correspondance MOP apparaît de la même façon comme
    sa propre ligne.
- **Immuabilité post-enclose durcie** (prérequis) :
  `AddCustomItem`/`DeleteCustomItem`
  (`internal/modules/cash_registers/repository.go`) ne vérifiaient que
  `closed`, jamais `enclosed` — rien n'empêchait de modifier les custom
  items d'un registre déjà verrouillé définitivement. Nouvelle méthode
  `isCashRegisterEditable` (`closed AND NOT enclosed`), utilisée
  uniquement par ces deux méthodes. `isCashRegisterClosed` reste inchangée
  pour `GetCashRegisterSummary` et `EncloseCashRegister`, qui doivent
  continuer de fonctionner sur un registre déjà enclosed (trouvé en
  écrivant les tests : réutiliser `isCashRegisterClosed` pour tout aurait
  cassé l'affichage du résumé d'un registre déjà clôturé).
- **Non traité (limite assumée)** : un registre ouvert en toute fin de
  mois et encore ouvert après minuit local pourrait contenir des
  commandes du mois suivant, comptées dans le réel du mauvais mois
  (ancrage sur `start_date`, cohérent avec l'ancrage `creation_date` des
  commandes ailleurs dans le module). Cas marginal, non traité par du
  code cette itération — un `log.Warn` signale les registres en dérive
  pour investigation manuelle.

### Rapport comptable — labels exclus en plus des codes MOP, `cash_registers_items` retiré du réel (2026-08-13)

- **Exclusion par libellé affiché** : `accountingExcludedChannelMOPs`
  (`STRIPE`/`UBER_EATS`/`DELIVEROO`) filtre en SQL le code MOP brut
  (`cri.mop`/`crci.label`) *avant* la jointure vers `labels` — un custom
  item à texte libre saisi littéralement "Uber Eats"/"Deliveroo"/
  "ScanNOrder" (au lieu du code MOP exact) passait donc au travers. Ajout
  d'un filtre applicatif `filterExcludedPaymentLabels`
  (`accounting/service.go`), en aval de `GetRealPaymentsData`, qui compare
  le libellé affiché en majuscule (`UBER EATS`/`DELIVEROO`/`SCANNORDER`)
  — garde-fou en plus du filtre SQL existant, pas un remplacement.
- **`cash_registers_items` totalement retiré de `GetRealPaymentsData`** :
  confirmé par le métier (Ilies, 2026-08-13) — un restaurateur ne peut
  enclose sa caisse qu'après avoir lui-même ressaisi le détail réel en
  `cash_registers_custom_items`, avec un écart proche de 0 exigé avant de
  pouvoir valider. `cash_registers_items` (instantané MOP automatique posé
  à la clôture, cf. entrée précédente) est donc redondant pour ce
  rapport : la requête de `GetRealPaymentsData` ne lit plus que
  `cash_registers_custom_items`, l'UNION vers `cash_registers_items` a été
  supprimée.
  - `GetTrustedEnclosedRegisterIDs` (comparaison frozen `cash_registers_items`
    vs live `payments`, garde-fou anti-dérive) **n'est pas concernée** :
    elle continue de s'appuyer sur `cash_registers_items` pour détecter
    qu'un paiement a été corrigé après l'enclose d'un registre — ce
    garde-fou reste nécessaire même si son contenu ne sert plus à
    afficher les montants du rapport.
  - Tests d'intégration Postgres mis à jour
    (`accounting/postgres_integration_test.go`) : les `mopPayment` seedés
    restent nécessaires pour exercer le frozen/live check, mais les
    montants attendus dans `GetRealPaymentsData` viennent désormais de
    `customItemSeed`/`AddCustomItem` ajoutés en parallèle, pas des
    `mopPayment` seuls.

### Traçabilité HACCP (photos + commentaire) — trou schéma Postgres cible (2026-07-23)

- Migration `067_haccp_traceability` (`haccp_traceability_records`/`haccp_traceability_photos`) ajoutée après la préparation Postgres, comme `planning_day_comments` en son temps (voir `docs/migration-postgres/26-planning-day-comments-integration.md`) : il manque encore sa traduction dans `04-schema-postgres-target.sql` (+ mise à jour `03-table-usage-audit.md`/`07-module-inventory.md`) — à rattraper avant le cutover Postgres (Phase 8), sinon la section traçabilité de `internal/modules/haccp/postgres_integration_test.go` échouera contre le Postgres de dev.

### Phase 1 — Fondation device_id enrôlement Kiosk (2026-07-22)

- **Contexte** : suite à l'audit `docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md`
  (aucun identifiant device stable n'existe côté API ni côté Flutter Kiosk —
  seul le `kiosk_id` serveur, perdu si le stockage local est effacé). Cette
  phase pose uniquement la fondation de capture, **sans** endpoint de
  ré-identification (`/kiosk/auth/reclaim` reste à faire).
- **`kiosks.device_id VARCHAR(191) NULL`** (migration `062_kiosks_device_id`) :
  pas de contrainte UNIQUE — l'unicité applicative (bloquer un reclaim sur
  device_id dupliqué plutôt que faire échouer une insertion) sera gérée par
  le futur endpoint. Colonne nullable, aucun backfill : les bornes déjà
  enrôlées restent à `NULL` et continuent sur le flow d'enrôlement classique.
  Ce n'est pas un secret — aucune valeur d'authentification n'est attachée à
  ce champ.
- **`EnrollRequest.DeviceID` optionnel** (compat ascendante) : chaîne vide ou
  absente → `NULL` stocké (pas une chaîne vide), pour ne jamais faire
  coïncider deux bornes sur un device_id "vide" au lieu d'un `NULL` (qui ne
  matche jamais rien dans une future recherche exacte) — voir
  `Service.EnrollDevice`, conversion en `*string` avant `Repository.CreateKiosk`.
  Aucun changement à `ValidateAccessToken`, `RefreshDeviceToken`, ni à aucun
  comportement d'auth existant ; aucune nouvelle route.
- **Côté Flutter (wello-kiosk) : reporté.** Le plan initial (réutiliser
  `platform_device_id_plus` comme `wello_resto_flutter`, cache dans une
  nouvelle clé secure-storage `kiosk_os_device_id` distincte de
  `AuthService.keyDeviceId`) est bloqué par un conflit de dépendances réel :
  `wakelock_plus ^1.6.1` (déjà utilisé par wello-kiosk, requis pour empêcher
  la mise en veille de la borne) exige `win32 ^6.0.1`, incompatible avec
  `device_info_plus ^11.3.0` (dépendance transitive de
  `platform_device_id_plus ^1.0.7`), qui exige `win32 ^5.5.3`.
  `wello_resto_flutter` n'a pas `wakelock_plus`, d'où l'absence de conflit
  là-bas. Un downgrade `wakelock_plus` → `^1.5.2` résout la contrainte
  (confirmé par `flutter pub get`, testé puis annulé) mais downgrade aussi
  `win32`, `package_info_plus` et `flutter_secure_storage_windows` en
  cascade — décision reportée à Ilies plutôt que tranchée unilatéralement.
  **Aucun fichier Flutter modifié** (tous les edits ont été passés puis
  annulés pour ne pas laisser le projet dans un état qui ne compile pas).
- **Prochaine étape (hors scope ici)** : trancher le conflit de dépendance
  côté wello-kiosk, puis reprendre la capture `device_id` client (même
  design que ci-dessus), avant d'attaquer `/kiosk/auth/reclaim`.

### Phase 2 — Endpoint `/kiosk/auth/reclaim` par device_id (2026-07-22)

- **Contexte** : suite de la Phase 1 (fondation `device_id`). Objectif :
  permettre à une borne dont le refresh token est perdu (stockage effacé,
  réinstallation) de retrouver son profil via `device_id`, sans réenrôlement
  manuel dans le cas courant — voir
  `docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md`.
- **Écart constaté avec le texte de la Phase 1 ci-dessus** (audit-first,
  signalé ici plutôt que corrigé silencieusement) : l'entrée Phase 1 déclare
  la capture `device_id` côté Flutter "reportée" à cause d'un conflit
  `wakelock_plus`/`platform_device_id_plus`. En pratique, `wello-kiosk` a
  depuis résolu ce conflit avec le package `android_id` (Android uniquement,
  aucune dépendance `win32`) — `AuthService.getOrGenerateOsDeviceId()` existe
  et fonctionne, documenté dans `wello-kiosk/docs/KIOSK_DECISIONS.md`
  ("Identifiant device OS (android_id)"). Cette phase s'appuie donc sur cette
  fondation déjà en place ; l'entrée Phase 1 de ce fichier n'a simplement
  jamais été mise à jour après cette résolution côté `wello-kiosk`.
- **Recherche des candidats par `device_id`** : `status IN ('active',
  'inactive')` uniquement — une borne `revoked` n'est jamais éligible et
  n'apparaît même pas dans la requête SQL (`Repository.
  FindKioskCandidatesByDeviceID`), pour ne pas laisser fuiter son existence.
  0 ligne ou >1 ligne (collision) → réponse HTTP identique
  (`kiosk_not_found`, 404) : le client ne distingue jamais les deux cas et
  retombe sur l'enrôlement classique dans les deux cas.
- **Réactivation silencieuse conditionnée au dernier heartbeat connu** :
  `last_heartbeat_at` < 30 jours → réémission de tokens sans aucune
  vérification de PIN (même si un PIN est configuré) ; `last_heartbeat_at`
  ≥ 30 jours ou `NULL` (borne jamais vue depuis son enrôlement) → PIN admin
  obligatoire. Constante `kioskReclaimSilentWindow` (30 jours), non
  configurable par env var pour cette phase.
- **PIN admin réutilisé tel quel** : `Service.verifyAdminPinCore` extrait la
  logique déjà existante de `VerifyAdminPin` (déchiffrement,
  comparaison à temps constant, lockout Redis 5 tentatives/30s par
  `kioskID`) — aucun lockout serveur dédié à `/reclaim`, le lockout propre à
  cet écran est géré côté app Flutter (voir plus bas).
- **Réutilisation de la ligne `kiosks` existante** : `ReclaimDevice` ne crée
  jamais de nouvelle borne — révoque tous les refresh tokens existants
  (`RevokeAllDeviceTokens`, même mécanisme d'hygiène qu'un `refresh` normal)
  puis en émet un nouveau, met à jour `last_heartbeat_at`/`last_ip`
  (`UpdateKioskLastSeenOnReclaim`, dédiée pour ne jamais écraser
  `app_version` avec une valeur vide — le client de reclaim ne la transmet
  pas), PIN admin inchangé (ni régénéré ni ré-exposé dans la réponse).
- **Endpoint public** (`POST /kiosk/auth/reclaim`, pas de Bearer, même
  famille que `/auth/enroll` et `/auth/token/refresh`), sans rate-limit par
  IP pour cette phase (décision explicite, à revisiter plus tard si besoin).
- **Nouveau code d'erreur** `kiosk_reclaim_pin_required` (401) ; 0/>1
  candidat réutilise `kiosk_not_found` (404) existant plutôt que d'en créer
  un distinct pour la collision.

### Statuts produits — fiabilisation backend + affichage/blocage SNO (2026-07-05)

- **Contexte** (audit du 2026-07-04) : `products.status` est une colonne texte
  libre. Valeurs effectives : `available`/`1` (commandable), `not_available`
  (toggle POS)/`0`, `out_of_stock`, `removed_from_menu` (soft-delete, filtré).
  Règle de vérité : commandable ⇔ statut ∈ {available, 1} (POS + pricing).
- **`UnavailableProductInfo.Status` int → string** : la requête
  `GetUnavailableProducts` retourne des statuts textuels — le `rows.Scan` en
  int échouait dès qu'un produit non-numérique était indisponible (pricing en
  erreur au lieu de la liste `unavailable_products`).
- **`ComponentUsage.Status` int → string** (models + module menu) : même bug,
  plus grave — le scan du menu (`GetMenuFromMerchantId`) échouait dès qu'un
  composant portait un statut textuel : **un ingrédient désactivé depuis le
  POS cassait le GetMenu entier** (menu 500 POS/SNO/Kiosk). Scans corrigés
  (menu/repository.go, orders_fetcher_builder.go). Les parseurs POS font déjà
  `json['status']?.toString()` — changement de type wire sans impact client.
- **`not_available` ajouté** : (a) aux checks composants de
  `GetUnavailableProducts` (orders) et `validateProductAvailability`
  (order_life_cycle) — un composant désactivé au POS ne bloquait pas les
  produits qui en dépendent ; (b) à `mapWelloStatusToAvailability` (menu) —
  la sync Uber Eats/Deliveroo était silencieusement sautée quand le POS
  désactivait un produit.
- **Garde CreateOrder SNO** (`scannorder/service.go`) : le pricing répond
  "success" même avec `unavailable_products` non vide, et le gate de création
  (`validateProductAvailability`) ne bloque que `out_of_stock` — un produit
  `not_available` pouvait être **commandé et payé** via SNO. La création SNO
  retourne désormais `{status: "unavailable_products", message: <noms>}`
  (même statut que le gate order_life_cycle).
- **Non traité (dette notée)** : le gate de création (partagé POS/Kiosk) ne
  bloque toujours que `out_of_stock` au niveau produit (asymétrie voulue :
  un staff POS peut encaisser un produit désactivé à la vente en ligne) ;
  le Kiosk n'inspecte pas `unavailable_products` (cf.
  KIOSK_VS_SCANNORDER_STRUCTS.md §propositions) ; `products.available`
  (PATCH /availability) reste sans effet sur le menu — à filtrer ou déprécier.
- Côté SNO : affichage Épuisé/Indisponible + blocage panier/checkout — voir
  `wello-resto-scannorder/docs/decisions.md` (entrée du 2026-07-05).

### Refonte page de suivi de commande SNO — carte temps réel + layouts (2026-07-02)

- `OrderTrackingPage` (repo `wello-resto-scannorder`) refondue : side sheet
  400px + carte interactive sur desktop (≥1024px), carte plein écran + bottom
  sheet `vaul` (3 snap points, safe-area iOS) sur mobile **et tablette**
  (justification : sous 1024px, un side sheet de 400px ne laisserait qu'une
  carte étroite en portrait — le layout tactile reste supérieur jusqu'à `lg`).
  Mode IN sans carte, panneau centré avec `pager_number` en bloc dominant.
- Suivi livreur branché sur `PublicDeliverySession` (inline dans `GET order`,
  polling 10s inchangé) : marqueur interpolé 30s le long de la polyline OSRM
  (port fidèle de `driver_marker_animation.dart`, constantes d'origine
  conservées dans `src/lib/geo.ts`), reroute sur déviation (75m / 2 points /
  90s min), ETA fourchette calculée côté client (segments non parcourus),
  rafraîchie en continu. `stops_before_you > 0` → rang affiché, pas d'ETA
  minute ni de polyline livreur→client. Staleness approximée côté client
  (position inchangée > 60s → marqueur grisé), en attendant l'exposition de
  `last_position_at` (amélioration listée au contrat).
- Types SNO étendus : `PublicDeliverySession`/`PublicDeliveryMan`,
  `Order.delivery_session`/`pager_number`, `OrderCustomer.customer_lat/lng`.
- Détail des choix créatifs : [audits/2026-07-02-order-tracking-page-design-decisions.md](audits/2026-07-02-order-tracking-page-design-decisions.md).

### Finalisation suivi livreur SNO — fix GetDeliverySessionByOrderID + contrat PublicDeliverySession (2026-07-02)

- **Fix `GetDeliverySessionByOrderID`** (`internal/modules/scannorder/repository.go`) :
  la requête n'avait ni `ORDER BY` ni filtre de statut. Après un re-dispatch
  d'une commande (session initiale `failed`/`canceled` côté livreur, nouvelle
  session créée), elle pouvait retourner n'importe quelle session historique
  arbitraire au lieu de la session active courante. Ajout d'un
  `ORDER BY ds.start_date DESC` (pas de colonne `created_at` sur
  `delivery_session`) + `WHERE ds.status != 'canceled'` (garde `active` et
  `done` — une session tout juste terminée doit rester consultable quelques
  minutes pour l'affichage post-livraison côté SNO) + `LIMIT 1`. Valeurs de
  `delivery_session.status` confirmées via la migration
  `035_delivery_session_status_normalization` : uniquement `active`/`done`/
  `canceled` depuis cette normalisation. Le seul appelant
  (`Service.GetOrderSNO`) gérait déjà le cas `nil`, aucun changement
  nécessaire côté appelant.
- **Contrat `PublicDeliverySession` documenté** dans
  [docs/api-contracts/public-delivery-session.md](api-contracts/public-delivery-session.md) :
  champs exposés/non-exposés, sources de données ETA côté SNO (restaurant/
  client/livreur), fréquences de polling recommandées (30s position livreur,
  10s reste du payload), et pattern d'interpolation du marqueur à reproduire
  côté SNO (référence `driver_marker_animation.dart` du POS Flutter). Prêt
  pour consommation par la refonte visuelle SNO. Pas de nouvel endpoint —
  `PublicDeliverySession` reste inline dans
  `GET /scannorder/{slug}/orders/{id}` (Option B, déjà validée par le
  hotfix RGPD ci-dessous).

### 🔴 HOTFIX RGPD — Fuite de données sur GET /scannorder/{slug}/orders/{order_id} (2026-07-02)

- **Problème** : l'endpoint public (client final non authentifié suivant sa
  commande) retournait la `delivery_session` interne complète inline. Son champ
  `orders[]` contenait **toutes les commandes de la tournée du livreur**, chacune
  avec le `Customer` complet des autres clients : noms, adresses, téléphones,
  emails, GPS et `customer_delivery_notes`. Non-conformité RGPD.
- **Correctif** : la `delivery_session` est désormais filtrée via un DTO dédié
  `scannorder.PublicDeliverySession` (+ `PublicDeliveryMan`). Il n'expose que la
  **position du livreur** (`lat`/`lng`/`status`), son **prénom** uniquement, le
  **statut** de la session, et un rang **non-identifiant** du stop du client
  (`stops_before_you` / `total_stops`, des compteurs — jamais les commandes).
  Réponse encapsulée dans `PublicSNOOrder` (embarque l'`Order` du client, dont
  les données propres restent légitimes, et écrase le champ `delivery_session`).
- **Séparation modèle interne / public** : le DTO public est distinct de
  `models.DeliverySession` (utilisé par les endpoints merchant authentifiés, qui
  continuent de voir la tournée complète). Un futur ajout de champ sur le modèle
  interne ne peut donc plus recréer la fuite automatiquement.
- Fin de la fuite de données personnelles des autres clients de la tournée.

### Upsell — Configuration produit complète (2026-07-01)

- Backend : GetUpsellProducts filtre désormais is_product_group = 1 (produits
  groupe non commandables seuls exclus de l'upsell).
- Backend : GetUpsell enrichit chaque produit via GetProductFromMerchantId
  (même fonction que l'endpoint fiche produit) — configuration complète
  (attributs, options, prix) retournée pour chaque suggestion.
- Backend : résultat mis en cache Redis, même TTL que GetMenu.
- Frontend : aucun changement — UpsellPopup.tsx était déjà câblé pour détecter
  les produits configurables (isConfigurable()) et ouvrir ProductModal si besoin.

### Phase B — Unification logique upsell (2026-07-01)

- SuggestedItem enrichi : configuration produit complète (attributs, options,
  prix par canal) retournée par GenerateUpsell pour toutes les plateformes.
- Nouveau handler SNO POST /scannorder/{slug}/upsell utilisant GenerateUpsell
  (Apriori/LLM/featured, contextuel au panier), payload PricingRequest réutilisé.
- Ancienne route GET /scannorder/{slug}/upsell marquée dépréciée, conservée
  temporairement (suppression prévue en Phase A résiduelle).
- Frontend SNO : useUpsell refactor sur le pattern usePricing (POST + debounce
  + queryKey dynamique sur le panier).
- Tracking non branché sur SNO — prévu en Phase C.

### Phase C — Tracking upsell SNO et Kiosk (2026-07-01)

- Migration : colonne channel ENUM('POS','SNO','KIOSK') ajoutée à
  upsell_suggestions (DEFAULT 'POS' pour rétro-compatibilité).
- GenerateUpsell / CreateSuggestion propagent désormais le canal
  jusqu'à la persistence (les 3 handlers passent leur canal explicitement).
- SNO et Kiosk : suggestion_id désormais retourné dans la réponse upsell,
  transporté par le front jusqu'à la création de commande, et peuplé dans
  UpsellSuggestionID du RequestObject.
- Frontend SNO : suggestion_id stocké dans useCart au moment de l'acceptation
  d'une suggestion (option A, cohérence avec POS Flutter).
- Kiosk : correctif symétrique appliqué (les lignes upsell_suggestions
  n'étaient jusqu'ici jamais rattachées à un order_id, l'ID étant
  silencieusement jeté).
- TrackAsync : non modifié — se déclenche automatiquement dès que
  UpsellSuggestionID est non-vide, tous canaux confondus.

### POS Flutter — Branchement product + suggestion_id (2026-07-02)

- UpsellItemDto (POS) enrichi d'un champ `product` optionnel (ProductDto
  complet, parsé via ProductResponse.fromJson) ; UpsellResultDto enrichi
  d'un `suggestionId`.
- Tap sur une suggestion : si `product` est présent, popup de configuration
  (options/groupe) ouverte comme pour un ajout depuis le menu, au lieu du
  ProductDto synthétique précédent (configuration vide hardcodée).
- `suggestion_id` stocké dans UpsellController uniquement à l'acceptation
  d'une suggestion (tap), pas à la simple réception de la liste ; transmis
  jusqu'à OrderDto.upsellSuggestionId puis OrderPayload.upsell_suggestion_id
  à la création de commande. Réinitialisé quand le panier est vidé ou la
  commande finalisée.
- **Limitation connue** : si l'utilisateur accepte deux suggestions
  provenant de batches upsell différents (un nouveau fetch a eu lieu entre
  les deux, donc un nouveau `suggestion_id` côté backend) avant de valider
  la commande, seule la dernière suggestion acceptée est trackée — le
  POS ne transmet qu'un seul `upsell_suggestion_id` par commande, reflet
  direct du modèle `RequestObject.UpsellSuggestionID` (un seul champ,
  pas une liste). Aucun contournement possible côté frontend sans
  évolution du modèle backend (ex. accepter une liste de suggestion_id).

### Homogénéisation des DTO upsell Kiosk (2026-07-01)

- `Service.GetUpsellSuggestions` (kiosk) retourne désormais directement
  `*upsell.UpsellResult` (même structure que `/orders/upsell` POS : `suggestions`,
  `source`, `suggestion_id`) au lieu des DTO dédiés `KioskUpsellSuggestion` /
  `KioskUpsellResponse`, supprimés (`internal/modules/kiosk/models.go`). Plus
  de mapping spécifique Kiosk à maintenir.
- Correction du bug de performance identifié en Phase B (audit
  `docs/audits/2026-07-01-upsell-v2.md`) : la boucle de construction des
  suggestions refaisait un appel `GetProductFromMerchantId` par suggestion,
  alors que `sugg.Product` est déjà chargé par `enrichWithProductConfig` en
  amont dans `GenerateUpsell`. Cet appel redondant est supprimé — `sugg.Product`
  est réutilisé tel quel.
- Nettoyage produit par canal appliqué à la volée avant sérialisation, pas en
  amont dans le cache : POS reste en mode brut (4 prix, `ProductEntry` non
  nettoyé, comportement inchangé). SNO applique `cleanProductForSNO`
  (inchangé). Kiosk applique une nouvelle fonction dédiée `cleanProductForKiosk`
  (`internal/modules/kiosk/service.go`) — distincte de `cleanProductPricesForKiosk`
  (conservée telle quelle, encore utilisée par `mapProductEntryToKioskProduct`
  pour `GetMenu`/`GetProduct`). `cleanProductForKiosk` a été écrite spécifiquement
  pour ce chantier : exposer un `ProductEntry` brut sans nettoyage aurait fuité
  des champs internes/sensibles (`cost_price`, `foodcost_percent`,
  `margin_percent`, `merchant_id`, indicateurs de sync Uber Eats/Deliveroo,
  etc.) au client Kiosk — `cleanProductPricesForKiosk` seule ne fait que
  collapser le prix, elle ne les retire pas. `cleanProductForKiosk` reprend les
  principes de `cleanProductForSNO` (retrait des mêmes catégories de champs),
  adaptés à la convention Kiosk IN/TAKE_AWAY (pas de DELIVERY).
- Effet de bord sur `source` : la réponse Kiosk renvoyait auparavant une valeur
  simplifiée (`"apriori"` / `"featured_fallback"`) ; elle expose désormais les
  valeurs brutes d'`upsell.Service` (`pattern`, `llm`, `cached_pattern`,
  `cached_llm`, `featured_fallback`, `disabled`), identiques à celles du POS.
  Le Flutter Kiosk ne fait aujourd'hui qu'afficher/logger `source`, sans
  brancher de logique dessus — changement sans impact fonctionnel connu, à
  vérifier si un futur usage du champ apparaît côté client.
- Effet de bord sur le filtrage : une suggestion dont l'enrichissement produit
  a échoué en amont (`sugg.Product == nil`, best-effort dans
  `enrichWithProductConfig`) est désormais exclue de la réponse Kiosk plutôt
  que retombée sur un second fetch individuel — même comportement que
  `scannorder.PostUpsell` pour la même situation.
- Dette technique : `KioskUpsellRequest` (`POST /kiosk/upsell`) ne transporte
  toujours pas de `fulfillment_type` — `GetUpsellSuggestions` reçoit `""`,
  qui tombe sans erreur sur le prix de base (IN) dans `cleanProductForKiosk`
  (pas de crash, mais un client TAKE_AWAY verra le prix sur place dans la
  suggestion tant que ce n'est pas câblé). À threader depuis le payload de
  la borne si le prix à emporter doit être exact dans le popup d'upsell.

### Kiosk Flutter — Branchement product + suggestion_id (2026-07-02)

- `UpsellSuggestion` (Kiosk) enrichi d'un champ `product` optionnel (`Product`
  complet, même modèle que `/kiosk/menu`) ; `UpsellResponse` enrichi d'un
  `suggestionId` racine (identifie le batch, pas une suggestion individuelle).
- `_selectSuggestion` utilise `suggestion.product` directement au lieu d'un
  appel systématique à `MenuController.findOrFetchProduct` ; fallback
  transitoire conservé si `product` est absent.
- `suggestion_id` stocké dans `UpsellController` au moment du **tap** sur une
  suggestion (pas à l'ajout effectif au panier, contrairement à SNO — le
  chemin "suggestion configurable" ouvre une route produit indépendante de
  l'écran upsell côté Kiosk, rendant la détection post-ajout fragile).
  Transmis jusqu'à `RequestObject.upsellSuggestionId`. Réinitialisé au
  prochain tap, à la commande finalisée avec succès, ou au panier vidé.
- **Même limitation connue que POS/SNO** : "dernière acceptée gagne", un seul
  `upsell_suggestion_id` par commande.
