# Synchronisation comptable (Pennylane et suivants)

Chantier du 2026-10-12. Chaque établissement peut relier **un** logiciel de
comptabilité choisi dans un catalogue (Pennylane pour l'instant). Chaque jour,
Wello Resto lui envoie l'**écriture de ventes** de la journée, construite à
partir de la clôture fiscale journalière.

Code : [internal/modules/accountingsync/](../internal/modules/accountingsync/)
(noyau), [providers/pennylane/](../internal/modules/accountingsync/providers/pennylane/)
(connecteur), tâche [internal/tasks/accounting_sync.go](../internal/tasks/accounting_sync.go),
migration [181](../migrations/todo/181_accounting_sync.up.sql).

## Décisions (Ilies, 2026-10-12)

| Sujet | Décision |
|---|---|
| Nature de ce qui est envoyé | **Écritures comptables** uniquement (pas de factures clients : un ticket de caisse n'a pas de client). |
| Source des chiffres | `fiscal_closures` (clôtures **DAY**) : scellées, immuables, déjà calculées. Aucun recalcul. |
| Outils par établissement | **Un seul** (clé primaire `merchant_id`). En changer = déconnecter puis reconnecter. |
| Début de la synchro | **Dès la connexion** : premier jour envoyé = jour local de la validation de la correspondance de comptes. La reprise d'historique viendra plus tard (voir « Limites »). |
| Authentification Pennylane | Jeton d'API **et** OAuth 2.0, dans le même connecteur. OAuth n'est proposé que si l'application de l'éditeur est configurée. |
| Clé de chiffrement | **Dédiée** : `ACCOUNTING_ENCRYPTION_KEY` (distincte de `KIOSK_PIN_ENCRYPTION_KEY`). |
| Droit requis | `platforms.manage` (configuration d'une plateforme externe). |

Une clôture DAY scellée ne change plus : une commande scellée ne peut pas être
rouverte (`internal/fiscal/reopen.go`, `ErrReopenOrderSealed`). Il n'y a donc
jamais d'écriture corrective à envoyer pour un jour déjà envoyé ; un
remboursement tardif figure dans la clôture du jour où il est fait.

## Architecture

```
fiscal_closures (DAY) ──► DailySales ──BuildEntry(Mapping)──► Entry ──► Provider.PushEntry ──► Pennylane
                                         (neutre, équilibrée)               (un connecteur par outil)
```

- **`Provider`** ([provider.go](../internal/modules/accountingsync/provider.go)) : l'interface que
  chaque outil implémente — `Info`, `Validate`, `Prepare`, `PushEntry`. Interfaces optionnelles :
  `OAuthProvider` (autorisation, rafraîchissement, révocation) et `EntryFinder` (retrouver une
  écriture déjà créée).
- **`Registry`** : le catalogue. Construit dans `cmd/api/routes.go`.
- **`BuildEntry`** ([entry.go](../internal/modules/accountingsync/entry.go)) : transforme une journée
  en écriture, **indépendamment de l'outil** (comptes exprimés en numéros du plan comptable).
- **`Service`** : connexion, correspondance de comptes, envoi, rafraîchissement des jetons.
- **`RunAccountingSync`** : tâche horaire, après `RunFiscalClosures`. Verrou par établissement
  (`pg_try_advisory_lock`) : ni deux instances ni une relance manuelle ne peuvent envoyer le même
  jour en même temps.

### Ajouter un outil

1. Créer `providers/<outil>/` avec un type qui implémente `accountingsync.Provider`
   (et `OAuthProvider` / `EntryFinder` si l'outil le permet).
2. L'ajouter au `NewRegistry(...)` de `cmd/api/routes.go`.

Rien d'autre ne change : ni la base, ni la planification, ni l'API, ni l'écran du back-office
(construit à partir de `GET /accounting-integrations`, qui liste le catalogue, les modes de
connexion et la correspondance de comptes suggérée de chaque outil).

## L'écriture

Pour la journée, `BuildEntry` produit :

| Côté | Compte | Montant |
|---|---|---|
| Crédit | produits (`sales_accounts`, par taux de TVA) | HT de chaque taux |
| Crédit | TVA collectée (`vat_accounts`, par taux) | TVA de chaque taux |
| Débit | encaissements (`payment_accounts`, par moyen de paiement) | paiements du jour |
| Débit ou crédit | compte d'écart (`difference_account`) | ventes − encaissements |

- Ventes = tickets et avoirs du jour (nets des remises), tels que scellés dans `vat_by_rate`.
- Encaissements = paiements **actifs** du jour ; les remboursements (`REFUND`) sont retranchés quel
  que soit leur signe en base ; les remises (`CURRENCY`, `PERCENTAGE`, `DISCOUNT`) sont exclues
  (ce n'est pas de l'argent encaissé).
- Le **compte d'écart** garantit une écriture toujours équilibrée : commande non réglée à la
  clôture, acompte d'un jour précédent, arrondi. L'écart est conservé dans
  `accounting_sync_items.difference` et affiché dans le journal.
- Un solde négatif (jour de remboursements) passe du côté opposé.
- Journée sans vente ni encaissement : `skipped`, rien n'est envoyé.
- Libellé : `Ventes Wello Resto du 11/10/2026 [WR-212-20261011]`. La référence
  `WR-<établissement>-<AAAAMMJJ>` permet de retrouver l'écriture chez l'outil.

### Correspondance de comptes (`Mapping`)

Propre à chaque établissement, indépendante de l'outil : `journal_code`, `sales_accounts`,
`vat_accounts`, `payment_accounts` (chacune avec une clé `default` obligatoire et des clés
précises : taux `"20"`, `"10"`, `"5.5"` ; moyen `"ES"`, `"CB"`, `"TR"`, `"UBER_EATS"`…),
`difference_account`. `PUT /accounting-integrations/mapping` la valide **chez l'outil** (journal et
comptes existent-ils ?) avant d'activer la synchronisation.

## Pennylane

API Company v2 (`/api/external/v2`).

| Besoin | Appel | Note |
|---|---|---|
| Valider le jeton, nom de la société | `GET /me` | accessible à tout jeton valide |
| Journal de ventes | `GET /journals` (pagination par curseur) | trouvé par `code`, insensible à la casse |
| Comptes | `GET /ledger_accounts?filter=[{field:number,operator:eq,value:…}]` | repli sur un parcours complet si le filtre est refusé (400) ; premier compte **actif** du numéro |
| Créer l'écriture | `POST /ledger_entries` | montants en **chaînes** décimales, débit = crédit sinon 422 |
| Retrouver une écriture | `GET /ledger_entries?filter=[{field:date,operator:eq,value:…}]` | puis recherche de `[WR-…]` dans le libellé |

Jeton d'API (créé par l'entreprise dans Paramètres > Connectivité > Développeurs, rôle
administrateur, abonnement Essential ou supérieur) : l'interface ne propose pas de droits un par un,
seulement **API V2 « Lecture seule » ou « Lecture et écriture »** — il faut « Lecture et écriture ».
Expiration au choix (1, 6, 12 mois ou illimité) : un jeton expiré fait passer la connexion en `error`.
Le jeton n'est affiché qu'une fois.

OAuth : `https://app.pennylane.com/oauth/authorize` et `/oauth/token`, scopes
`ledger_entries:all journals:readonly ledger_accounts:readonly`. Jeton d'accès 24 h ; jeton de
rafraîchissement 90 jours, **à usage unique** (chaque usage invalide l'ancien).

### Garde-fous

- **Pas d'idempotence côté Pennylane** : rejouer un `POST` créerait un doublon. D'où :
  - un envoi dont l'issue est inconnue (délai dépassé, 5xx, réponse illisible) est classé
    `AmbiguousError` ; on cherche d'abord l'écriture (`EntryFinder`) ; si on ne peut pas conclure,
    le jour passe en **`uncertain`** et n'est **jamais renvoyé automatiquement** — l'utilisateur
    vérifie dans Pennylane puis relance depuis le back-office ;
  - un refus avéré (4xx) est un `failed` : retenté chaque heure, abandonné après 72 essais ;
  - unicité `(merchant_id, provider, period_start)` : un jour n'a qu'une ligne de journal.
- **Jetons à usage unique** : le rafraîchissement se fait sous verrou de ligne
  (`SELECT … FOR UPDATE`), avec relecture après verrou, et le nouveau jeton est écrit **avant** tout
  autre appel. Un `invalid_grant` passe la connexion en `error` (reconnexion nécessaire).
- **Identifiants** : JSON chiffré en AES-256-GCM (`helpers.SecretBox`), jamais renvoyés par l'API.
  Le paramètre `state` OAuth est chiffré et daté (15 min) : le retour (public) du navigateur ne
  peut ni être forgé ni rejoué pour un autre établissement.
- **Cadence** : 300 ms entre deux envois (Pennylane limite à 25 requêtes / 5 s par jeton).

## Statuts

Connexion : `setup` (connectée, correspondance à valider) → `active` ↔ `error` (identifiants
refusés).
Journal des envois : `sent`, `failed` (retenté), `uncertain` (à vérifier), `skipped` (jour vide),
`abandoned` (relance manuelle).

## API (`platforms.manage`)

| Route | Rôle |
|---|---|
| `GET /accounting-integrations` | catalogue + connexion de l'établissement |
| `POST /accounting-integrations/{provider}/connect/token` | connexion par jeton d'API `{"token": "…"}` |
| `GET /accounting-integrations/{provider}/connect/oauth` | adresse d'autorisation OAuth `{"url": "…"}` |
| `GET /accounting-integrations/oauth/callback` | retour OAuth (public) → redirige vers `ACCOUNTING_RETURN_URL?accounting=ok\|error&reason=…` |
| `PUT /accounting-integrations/mapping` | correspondance de comptes (validée chez l'outil), active la synchro |
| `GET /accounting-integrations/items` | journal des 100 derniers jours envoyés |
| `POST /accounting-integrations/sync` | « Synchroniser maintenant » : envoie tout de suite les journées en attente (même verrou et mêmes garde-fous que la tâche horaire) ; renvoie `sent`, `failed`, `uncertain`, `busy` |
| `POST /accounting-integrations/items/{date}/retry` | relance d'un jour `failed` / `uncertain` / `abandoned` |
| `DELETE /accounting-integrations` | déconnexion (révocation OAuth au mieux ; le journal est conservé) |

Erreurs utiles à l'écran : `invalid_mapping` (400), `unknown_accounts` (422, avec la liste),
`credentials_rejected` (422), `already_connected` (409), `not_active` (409), `not_configured` (503, clé de chiffrement
absente).

## Variables d'environnement

| Variable | Rôle |
|---|---|
| `ACCOUNTING_ENCRYPTION_KEY` | **Obligatoire** pour toute connexion. `openssl rand -base64 32`. Ne jamais la changer : les identifiants déjà enregistrés deviendraient illisibles. |
| `PENNYLANE_CLIENT_ID`, `PENNYLANE_CLIENT_SECRET`, `PENNYLANE_REDIRECT_URI` | Application OAuth de l'éditeur (obtenue auprès de l'équipe Partenariats de Pennylane). Tant qu'ils ne sont pas tous renseignés, seul le jeton d'API est proposé. `PENNYLANE_REDIRECT_URI` = `https://<api>/accounting-integrations/oauth/callback`. |
| `PENNYLANE_BASE_URL` | Défaut `https://app.pennylane.com` ; pour un environnement de test. |
| `ACCOUNTING_RETURN_URL` | Page du back-office où revenir après l'autorisation OAuth. Vide : réponse JSON. |

## Mise en production

1. Appliquer la migration **181** (par vous, hors du code).
2. Définir `ACCOUNTING_ENCRYPTION_KEY` (la première connexion échoue en `not_configured` sans elle ;
   un avertissement au démarrage le signale).
3. Déployer l'API. Sans connexion active, la tâche horaire ne fait rien.
4. Déployer le back-office (page Comptabilité).

Tests : `go test ./internal/modules/accountingsync/...` ; intégration Postgres, **après la
migration 181** : `POSTGRES_URL=… go test -tags postgres_integration ./internal/modules/accountingsync/`.

## À valider sur le sandbox Pennylane (établissement 2) avant le pilote (212)

Le connecteur est écrit d'après la documentation publique et testé contre un faux serveur ; ces
points n'ont **pas** été vérifiés contre l'API réelle :

1. `GET /me` renvoie bien `company.name`.
2. Le filtre `operator: eq` sur `ledger_accounts.number` est accepté (sinon repli sur le parcours complet, déjà prévu).
3. Le filtre `date` / `eq` sur `ledger_entries` est accepté — sinon `FindEntry` échoue et un envoi
   ambigu passe en `uncertain` (comportement sûr, mais manuel).
4. L'écriture créée apparaît dans le bon journal, équilibrée, avec les bons comptes ; les comptes
   dont le numéro existe en plusieurs identifiants (paramétrage de TVA) sont bien choisis.
5. Le format d'envoi de l'échange de jeton OAuth (formulaire) — à confirmer dès réception des
   identifiants d'application.

## Limites et reste à faire

- **Reprise d'historique** : volontairement reportée. Elle ne pourra pas s'appuyer sur
  `fiscal_closures` pour les jours antérieurs à la première clôture de l'établissement
  (`cmd/backfill_fiscal_closures` ne recule pas une chaîne existante) ; il faudra une autre source
  (tickets et paiements). Le champ `sync_from` et l'unicité du journal préparent ce chantier.
- **Back-office** : fait (wello-back-office, page `/integrations/accounting`, menu « Canaux et
  Plateformes > Comptabilité », droit `platforms.manage`) : choix de l'outil, connexion par jeton ou
  OAuth, correspondance de comptes pré-remplie par `suggested_mapping`, journal des envois et relance
  (confirmation avant de relancer un jour « à vérifier »), déconnexion.
  Pour le retour OAuth, définir `ACCOUNTING_RETURN_URL` = `https://<back-office>/integrations/accounting`.
- **Mode de clôture manuel** (établissements 2 et 212) : l'export comptable mensuel repose sur le
  réel des registres, les clôtures fiscales sur les tickets et paiements enregistrés. Un écart de
  caisse peut donc séparer les deux ; il apparaît dans le compte d'écart. À comparer sur le pilote
  avant de l'activer pour d'autres.
- Une seule devise (EUR) ; pas de pièces jointes (le Z n'est pas joint à l'écriture).
- Pourboires : non gérés (aucun n'existe dans le système aujourd'hui).
