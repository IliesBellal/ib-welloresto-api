# Stripe Connect — encaissement ScanNOrder

Compte Stripe Connect **Express** par marchand, ouvert depuis le back-office
(page ScanNOrder → « Activer »). Les paiements ScanNOrder sont des *direct
charges* sur ce compte, la commission WelloResto est prélevée en
`application_fee_amount` (`internal/infrastructure/stripe/checkout.go`).

## Processus

1. Back-office → `POST /integrations/scannorder/onboarding`
   (`integrations.Service.CreateScanNOrderOnboarding`).
   - Pas de compte : création Express (FR, `card_payments` + `transfers`),
     pré-remplie avec le profil de l'établissement (voir plus bas), id stocké
     dans `stripe_accounts`.
   - Compte existant : profil réaligné (`UpdateAccountProfile`), un échec est
     journalisé sans bloquer.
   - Renvoie un AccountLink `account_onboarding` (`STRIPE_ONBOARDING_RETURN_URL`
     / `STRIPE_ONBOARDING_REFRESH_URL`).
2. Le marchand remplit l'onboarding Stripe : identité, type d'entreprise,
   **IBAN** (requis pour `payouts_enabled`).
3. Webhook `account.updated` (`webhook/stripe.HandleAccountUpdated`) :
   `stripe_accounts.verification_status` = `verified` si charges + payouts
   activés, et `scannorder_settings.activated = true` dès que
   `details_submitted && charges_enabled`.
4. Compte bancaire ensuite : onglet « Compte bancaire » →
   `GET /integrations/stripe/bank-accounts` (lecture) et
   `POST /integrations/stripe/bank-account-link` (modification).

## Décisions (2026-10-19)

**Nom du compte = nom de l'établissement.** `ConnectAccountProfile` pré-remplit :
- `business_profile.name` = `merchant.fullName` (nom affiché dans le dashboard
  Connect, sur Checkout) ;
- `settings.payments.statement_descriptor` dérivé du même nom
  (`StatementDescriptor` : majuscules, sans accent, 5–22 caractères latins ;
  omis s'il n'est pas valide, Stripe le demande alors à l'onboarding) ;
- `business_profile.mcc` = 5812 (restaurants), `business_profile.url` = URL
  publique de la boutique ScanNOrder si le marchand a un QR principal ;
- `email` = `merchant.email`, à défaut celui de l'utilisateur connecté
  (création seulement : c'est l'identifiant du dashboard Express) ;
- `metadata.merchant_id`.

La raison sociale (`company.name`) n'est pas pré-remplie : elle diffère
souvent du nom commercial et c'est au marchand de la saisir.

**`business_type` n'est plus imposé à `company`** : un auto-entrepreneur doit
pouvoir choisir « entreprise individuelle » à l'onboarding.

**Lien de modification du compte bancaire.** Un AccountLink `account_update`
n'est accepté que pour les comptes dont la plateforme collecte les
informations (`controller.requirement_collection = application`, Custom). Pour
les comptes Express, `CreateBankAccountLink` renvoie :
- onboarding pas encore soumis → lien `account_onboarding` (qui demande l'IBAN) ;
- sinon → login link du dashboard Express, où le marchand gère son IBAN.

L'ancien comportement (`account_update`) est conservé pour un éventuel compte
Custom.

## Recette (mode test Stripe)

- Activer ScanNOrder depuis le back-office d'un marchand sans compte : le
  compte créé porte le nom de l'établissement et le libellé de relevé.
- Relancer l'onboarding d'un compte existant : nom réaligné.
- Onglet « Compte bancaire » avant et après onboarding : redirection vers
  l'onboarding, puis vers le dashboard Express.

## Reste à faire

- Garde-fou « compte Stripe prêt » avant toute prise de commande (voir le
  point 2 de l'audit du 2026-10-19 : activation possible sans compte depuis le
  POS et `PATCH /integrations/scannorder`, commandes à l'avance qui contournent
  `activated`, commande créée avant l'échec du Checkout).
- Vérification de signature du webhook Stripe (non implémentée).
- Désactivation automatique quand Stripe restreint le compte.
