# Accorder une période d'essai à un nouvel établissement

Cas typique : l'établissement vient de créer son compte, le back-office lui réclame un moyen de
paiement (mandat SEPA) avant d'activer l'accès, et il n'a encore souscrit aucun produit. On lui
donne un accès complet gratuit pendant N jours sans attendre de moyen de paiement, via une
dérogation commerciale (`subscription_overrides`).

> Pas d'écran back-office pour ça aujourd'hui : c'est un appel API, à faire au clavier
> (curl/Postman) avec un compte ayant `users.is_platform_staff = true`.

## Procédure

1. **Récupérer l'ID de l'établissement** (`merchant_id`), par exemple depuis le back-office ou la
   base.

2. **Créer la dérogation** :

   ```
   POST /v1/admin/merchants/{merchant_id}/overrides
   Authorization: <token d'un compte platform staff>
   ```
   ```json
   {
     "kind": "price",
     "target": "0",
     "reason": "commercial",
     "note": "Essai 14 jours - nouvel établissement",
     "trial_ends_at": "2026-11-01T00:00:00Z"
   }
   ```

   - `target` = prix en centimes sous forme de chaîne, `"0"` = gratuit.
   - `reason` doit être l'une des valeurs fermées : `commercial`, `test`, `partenaire`,
     `migration`, `geste`.
   - `trial_ends_at` (RFC3339) est le champ qui compte ici — c'est lui qui déclenche les rappels et
     la bascule automatique à l'échéance (voir plus bas). Ne pas confondre avec `expires_at`
     (un marqueur passif, sans effet automatique).

3. **Effet immédiat** : `merchant.activation_state` passe à `LIVE`, `went_live_at` est posé,
   `subscriptions.status` passe à `active` et `override_price_cents = 0`. L'établissement a accès
   à la plateforme sans moyen de paiement ni produit souscrit.

4. **Vérifier** : `GET /v1/admin/overrides` liste les dérogations actives, triées par ancienneté.

## À l'échéance (`trial_ends_at`)

Géré automatiquement par le cron horaire (`RunTrialExpiryCheck`) :

- **J-7 et J-1** : un e-mail de rappel est envoyé au propriétaire de l'établissement (template
  Brevo `trial_expiry_reminder.html` — vérifier qu'il existe bien côté Brevo).
- **À l'échéance** :
  - si un mandat SEPA actif a été renseigné dans l'intervalle, la dérogation est révoquée
    silencieusement et l'abonnement réel (déjà en place) prend le relais — rien à faire.
  - sinon, la dérogation est révoquée **et** l'établissement retombe en `SETUP` (pas suspendu).

## Arrêter l'essai avant terme

```
DELETE /v1/admin/overrides/{override_id}
```

Ne révoque que la dérogation elle-même — ne touche pas `activation_state` ni
`override_price_cents`. Si l'établissement doit repasser en `SETUP` immédiatement, c'est à faire à
la main (pas automatisé par cette route).

## Partie 2 — directement en SQL (sans passer par l'API)

Même procédure, pour quand l'API n'est pas accessible (script, intervention base). Reproduit
exactement ce que fait `subscriptions.Service.CreateOverride` pour `kind='price'` avec un
`trial_ends_at` : une seule transaction, dans cet ordre précis.

`created_by` doit être le `user_id` d'un compte `is_platform_staff = true` (traçabilité de qui a
accordé la dérogation) :
```sql
SELECT user_id, name, email FROM users WHERE email = '<votre email>';
```

Puis :
```sql
BEGIN;

-- 1. Dérogation (ligne d'audit)
INSERT INTO subscription_overrides
    (id, merchant_id, kind, target, reason, note, created_by, trial_ends_at)
VALUES (
    'sbov-' || gen_random_uuid(),
    '<merchant_id>',
    'price',
    '0',                                    -- target : prix en centimes, "0" = gratuit
    'commercial',                           -- reason : commercial | test | partenaire | migration | geste
    'Essai 14 jours - nouvel établissement',
    '<user_id du membre staff>',
    '2026-11-01T00:00:00+00'                -- trial_ends_at : échéance de l'essai
);

-- 2. Effet : tarif de grille remplacé par 0
UPDATE subscriptions SET override_price_cents = 0 WHERE merchant_id = '<merchant_id>';

-- 3. Effet : passage en LIVE immédiat (ne touche rien si déjà passé LIVE par ailleurs)
UPDATE merchant
SET activation_state = 'LIVE', went_live_at = now()
WHERE id::text = '<merchant_id>' AND went_live_at IS NULL;

UPDATE subscriptions SET status = 'active' WHERE merchant_id = '<merchant_id>';

COMMIT;
```

**Vérifier** :
```sql
SELECT id, kind, target, reason, trial_ends_at, revoked_at FROM subscription_overrides WHERE merchant_id = '<merchant_id>' ORDER BY created_at DESC;
SELECT activation_state, went_live_at FROM merchant WHERE id::text = '<merchant_id>';
SELECT status, override_price_cents FROM subscriptions WHERE merchant_id = '<merchant_id>';
```

**Arrêter l'essai avant terme** (équivalent du `DELETE /v1/admin/overrides/{id}`) :
```sql
UPDATE subscription_overrides SET revoked_at = now() WHERE id = '<override_id>' AND revoked_at IS NULL;
```
Même limite qu'en API : ça ne touche ni `activation_state` ni `override_price_cents` — à remettre à
la main si l'établissement doit repasser en `SETUP` immédiatement.

**Ne rien faire pour l'échéance** : le cron horaire (`RunTrialExpiryCheck`) la gère tout seul (rappels
J-7/J-1, puis révocation + retour en `SETUP` si aucun mandat SEPA actif) — pas besoin de SQL pour ça,
sauf si les tâches planifiées sont arrêtées pour une raison quelconque.
