# ScanNOrder : carte consultable sans Stripe (2026-11-15)

## Décision

Dès sa création (`pos.InitMerchantSatellites` : QR principal + `scannorder_settings`),
un établissement a une carte ScanNOrder consultable, panier compris. Stripe ne sert
qu'à **encaisser** : sans compte Stripe Connect, aucune commande n'est possible
(pas de checkout). Un établissement qui veut seulement afficher sa carte est donc
équipé sans rien faire.

Trois notions indépendantes :

| Notion | Source | Effet |
|---|---|---|
| **Carte affichée** | `scannorder_settings.storefront_enabled` (migration 216, vrai par défaut) | Faux : `GET /scannorder/{slug}` répond `storefront_disabled` ; hors sitemap, hub et référencement. |
| **Commande en ligne** (commutateur) | `scannorder_settings.activated` | Posé à vrai par le webhook Stripe (`account.updated`, `details_submitted && charges_enabled`), puis commutateur back-office (`PATCH /integrations/scannorder {active}`) ou POS (`PATCH /pos/settings/scannorder`). |
| **Ouvert / fermé** | `merchant_parameters.is_open` (commutateur de la caisse, `PATCH /pos/status`), horaires, fériés, congés, `closed_until` | Statut affiché et refus `pos_closed` des commandes immédiates. N'est **plus** lié à `activated` : avant, un établissement non activé apparaissait « Fermé » en permanence. |

**Commande possible** (`scannorder.onlineOrderingEnabled`, `snoOnlineOrderingSQL`) =
carte affichée **et** `activated` **et** compte Stripe enregistré
(`stripe_accounts.account_id` non vide) **et** abonnement non suspendu. Exposé :
`merchant.online_ordering_enabled` (`GET /scannorder/{slug}`), sur chaque
établissement du hub, et dans `GET /integrations/scannorder`
(`online_ordering_enabled`, `storefront_enabled`, `stripe_account_status`).
`CreateOrderSNO` refuse sinon avec `online_ordering_unavailable`, **avant toute
écriture**. Avant ce contrôle, une commande sur place ou programmée (qui échappent à
la vérification d'ouverture) d'un établissement sans compte Stripe était insérée,
puis `CreateCheckoutSession` déréférençait un `AccountID` nil (panic).

### Commutateurs de la caisse (vérifié dans `wello_resto_flutter`)

- **Ouvert / fermé** : `PATCH /pos/status` → `merchant_parameters.is_open` seul
  (`POSRepository.UpdatePOSStatus`). Sans effet sur l'affichage de la carte ni sur
  le droit de commander ; il continue de fermer l'établissement (statut « Fermé »,
  `pos_closed`) exactement comme avant.
- **ScanNOrder** : `PATCH /pos/settings/scannorder` écrit `activated`. La caisse
  Flutter actuelle ne l'appelle pas. S'il était activé sans compte Stripe, la
  commande reste impossible (garde-fou ci-dessus).

## Requêtes (Postgres)

Établissements qui **pourront commander** après le déploiement (même règle que
`onlineOrderingEnabled` ; `storefront_enabled` vaut vrai partout juste après la
migration 216, la requête tourne donc aussi avant elle) :

```sql
SELECT CAST(m.id AS TEXT)      AS merchant_id,
       m.fullname              AS etablissement,
       sa.verification_status  AS statut_stripe,
       (SELECT string_agg(DISTINCT s.status, ', ')
          FROM subscriptions s WHERE s.merchant_id = snos.merchant_id) AS abonnement,
       EXISTS (SELECT 1 FROM subscriptions s
                WHERE s.merchant_id = snos.merchant_id
                  AND (s.status = 'canceled' OR s.scannorder_enabled = FALSE)) AS abonnement_a_verifier
FROM merchant m
JOIN scannorder_settings snos ON snos.merchant_id = CAST(m.id AS TEXT)
JOIN stripe_accounts sa       ON sa.merchant_id = snos.merchant_id AND TRIM(sa.account_id) <> ''
WHERE snos.activated = TRUE
  AND NOT EXISTS (SELECT 1 FROM subscriptions s
                   WHERE s.merchant_id = snos.merchant_id AND s.status = 'suspended')
ORDER BY m.fullname;
```

- `statut_stripe = 'action_required'` : compte dont les virements ne sont pas
  (ou plus) activés. Le paiement passe tant que Stripe accepte les encaissements,
  mais la base ne le sait pas : à surveiller.
- `abonnement_a_verifier` : abonnement résilié ou module ScanNOrder retiré. Ces
  établissements commandent aujourd'hui et commanderont encore (seule la
  suspension coupe la commande, comme avant) ; ils sont en revanche absents du
  sitemap et du hub (`snoSubscriptionBlocked`, inchangé).

Établissements **activés aujourd'hui qui ne pourront plus commander** (sans compte
Stripe, ou suspendus). Leurs commandes échouaient déjà au paiement :

```sql
SELECT CAST(m.id AS TEXT) AS merchant_id, m.fullname AS etablissement,
       sa.account_id IS NULL OR TRIM(sa.account_id) = '' AS sans_compte_stripe,
       EXISTS (SELECT 1 FROM subscriptions s
                WHERE s.merchant_id = snos.merchant_id AND s.status = 'suspended') AS suspendu
FROM merchant m
JOIN scannorder_settings snos ON snos.merchant_id = CAST(m.id AS TEXT)
LEFT JOIN stripe_accounts sa  ON sa.merchant_id = snos.merchant_id
WHERE snos.activated = TRUE
  AND (sa.account_id IS NULL OR TRIM(sa.account_id) = ''
       OR EXISTS (SELECT 1 FROM subscriptions s
                   WHERE s.merchant_id = snos.merchant_id AND s.status = 'suspended'))
ORDER BY m.fullname;
```

## Déploiement

1. Migration `216_scannorder_settings_storefront_enabled` **avant** l'API (l'API lit
   la colonne).
2. API.
3. Front ScanNOrder et back-office (ils lisent les nouveaux champs ; un front
   ancien reste fonctionnel, la commande refusée remonte en erreur générique).

## Limites connues

- `PATCH /pos/settings/scannorder` et le webhook Stripe n'invalident pas le cache
  vitrine : effet sous `ScannorderMerchantTTL` (2 min). Le back-office, lui,
  l'invalide.
- Le webhook remet `activated` à vrai à chaque `account.updated` d'un compte
  validé, même si l'établissement avait coupé la commande en ligne (comportement
  antérieur, inchangé).
- Un compte Stripe enregistré mais pas encore validé (onboarding abandonné) avec
  `activated` forcé à vrai à la main : la commande est créée puis le checkout
  échoue chez Stripe (`error_004`), comme avant.
