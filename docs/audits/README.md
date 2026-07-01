# Index des audits

Audits ponctuels réalisés lors d'échanges avec Claude Code, archivés ici pour référence future (à citer dans un prompt plutôt qu'à refaire l'analyse). Convention de nommage : `YYYY-MM-DD-sujet.md`.

Documentation vivante (non ponctuelle) : voir [../order-lifecycle.md](../order-lifecycle.md), qui reste à la racine de `docs/`.

| Audit | Pitch | Statut |
|---|---|---|
| [2026-07-01-supabase-suppression-mapping.md](2026-07-01-supabase-suppression-mapping.md) | Suppression de Supabase et mapping des entités Supabase → API interne. | ⚠️ Contenu à restaurer |
| [2026-07-01-cart-pricing.md](2026-07-01-cart-pricing.md) | Audit du panier et du calcul de pricing. | ⚠️ Contenu à restaurer (fichier source `docs/audit-cart-pricing.md` introuvable) |
| [2026-07-01-brand-status.md](2026-07-01-brand-status.md) | Valeurs possibles de `brand_status` et déclencheurs qui les modifient côté backend. | ⚠️ Contenu à restaurer |
| [2026-07-01-cancel-order-sno.md](2026-07-01-cancel-order-sno.md) | 5 points d'attention sur `CancelOrderSNO` (fenêtre d'annulation SNO). | ⚠️ Contenu à restaurer |
| [2026-07-01-merchant-approval-webhook-stripe.md](2026-07-01-merchant-approval-webhook-stripe.md) | `merchant_approval` écrasé par le webhook Stripe (P7) — cause et impact. | ⚠️ Contenu à restaurer |
| [2026-07-01-google-places-autocompletion.md](2026-07-01-google-places-autocompletion.md) | Intégration Google Places pour l'autocomplétion d'adresse. | ⚠️ Contenu à restaurer |
| [2026-07-01-validation-attributs-quantity.md](2026-07-01-validation-attributs-quantity.md) | Validation des attributs de type QUANTITY dans la ProductModal. | ⚠️ Contenu à restaurer |
| [2026-07-01-upsell-v1.md](2026-07-01-upsell-v1.md) | Fix initial de `/scannorder/{slug}/upsell` : sélection `is_popular`, filtre `is_product_group`, construction produit via `GetProductFromMerchantId`, cache Redis. | ⚠️ Contenu à restaurer |
| [2026-07-01-upsell-v2.md](2026-07-01-upsell-v2.md) | Comparatif complet `/orders/upsell` (POS) vs `/scannorder/upsell` (SNO) + faisabilité d'un cache Redis par produit et d'une unification de la logique upsell (phases A/B/C). | ✅ Complet |

## Audits marqués "Contenu à restaurer"

Ces audits ont été réalisés lors d'échanges antérieurs non présents dans le contexte de la session qui a créé ce dossier. Pour les compléter, fournir le contenu original (copier-coller de la conversation source) — voir la note dans chaque fichier.
