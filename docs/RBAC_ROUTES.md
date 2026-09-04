# Inventaire des routes — RBAC lot 2

Généré depuis `cmd/api/routes.go` au moment de la bascule `RequirePermission(key permission.Key)`
(RBAC lot 2). Une ligne par route déclarée dans `SetupRoutes`. Colonnes :

- **Auth** : `aucune` (publique), `authMiddleware` (jeton utilisateur), `KioskAuth` (device kiosk,
  distinct — voir `internal/middleware/kiosk_auth.go`), ou un rate-limit (`httprate`, sans jeton).
- **Droit requis** : la `permission.Key` posée par `middleware.RequirePermission(...)`,
  ou **`aucun`** — authentifiée mais sans garde RBAC. `RequireAdmin` (détient tous les droits —
  hors catalogue) a existé jusqu'à RBAC lot 11 phase 4 ; ses deux derniers consommateurs
  (`/users/{id}/force-reset-password`, `DELETE /users/{id}/merchant-link`) sont passés sous
  `staff.manage` et `RequireAdmin` a été retirée du code — plus aucune ligne de ce tableau ne
  devrait porter cette valeur.

Les lignes **`authMiddleware` / `aucun`** sont la liste de ce qui reste à trancher : ni un bug ni
un oubli de cette bascule (le principe directeur du lot 2 est de ne changer aucune décision
d'autorisation existante), mais l'état réel du dépôt, rendu visible plutôt qu'enfoui dans le
routeur.

RBAC lot 2.5 a retiré la garde de vérification email/téléphone (`IsEmailVerified`/
`IsTelVerified`, factorisées dans `forbiddenCode`) — ce n'était pas un droit RBAC mais un état
d'établissement détourné en décision d'autorisation. Voir
`docs/RBAC_VERIFICATION_RETIREE.md` pour le détail de ce qui a été retiré, pourquoi, et ce qu'il
reste à concevoir.

---

## `/health`, `/test`, `/webhooks` — public

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/health` | aucune | aucun |
| GET | `/test/test-mailer` | aucune | aucun |
| GET | `/test/test-sms` | aucune | aucun |
| POST | `/test/notification` | aucune | aucun |
| POST | `/test/deliveroo/brandID` | aucune | aucun |
| POST | `/test/deliveroo/upload-menu` | aucune | aucun |
| POST | `/test/deliveroo/unavailabilities` | aucune | aucun |
| POST | `/test/deliveroo/9` … `/17` (9 routes) | aucune | aucun |
| POST | `/webhooks/uber-eats` | aucune (signature provider) | aucun |
| POST | `/webhooks/deliveroo/orders` | aucune (signature provider) | aucun |
| POST, GET | `/webhooks/deliveroo/menu` | aucune (signature provider) | aucun |
| POST | `/webhooks/stripe` | aucune (signature Stripe) | aucun |
| POST | `/webhooks/brevo/sms-reply` | aucune (signature Brevo) | aucun |
| POST | `/webhooks/brevo/events` | aucune (token Brevo dans le service) | aucun |

## `/external`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/external/routes` | authMiddleware | aucun |

## `/auth`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET, POST | `/auth/login` | aucune | aucun |
| GET | `/auth/mfa/fallback-sms` | aucune | aucun |
| POST | `/auth/send-verification` | aucune | aucun |
| POST | `/auth/verify` | aucune | aucun |
| POST | `/auth/forgot-password` | aucune (perdu son mot de passe) | aucun |
| POST | `/auth/reset-password` | aucune (token de reset dans le body) | aucun |
| POST | `/auth/pin` | authMiddleware | aucun |
| POST | `/auth/pin/set` | authMiddleware | aucun |
| POST | `/auth/pin/reset` | authMiddleware | `staff.manage` |

## `/users`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/users/profile` | authMiddleware | aucun |
| PATCH | `/users/profile` | authMiddleware | aucun |
| POST | `/users/profile/avatar` | authMiddleware | aucun |
| GET | `/users/notifications` | authMiddleware | aucun |
| GET | `/users/` | authMiddleware | `staff.manage` |
| POST | `/users/` | authMiddleware | `staff.manage` |
| POST | `/users/create` | authMiddleware | `staff.manage` |
| GET | `/users/linkable-search` | authMiddleware | `staff.manage` |
| GET | `/users/{id}` | authMiddleware | `staff.manage` |
| POST | `/users/{id}/merchant-link` | authMiddleware | `staff.manage` |
| GET | `/users/{id}/rights` | authMiddleware | `staff.manage` |
| PUT | `/users/{id}/rights` | authMiddleware | `staff.manage` |
| GET | `/users/{id}/member` | authMiddleware | `staff.manage` |
| PATCH | `/users/{id}/member` | authMiddleware | `staff.manage` |
| POST | `/users/{id}/force-reset-password` | authMiddleware | `staff.manage` (RBAC lot 11 phase 4 — était `RequireAdmin`, retirée) |
| DELETE | `/users/{id}/merchant-link` | authMiddleware | `staff.manage` (RBAC lot 11 phase 4 — était `RequireAdmin`, retirée) |
| GET | `/users/{user_id}/location` | authMiddleware | aucun |
| PATCH | `/users/location` | authMiddleware | aucun |
| PATCH | `/users/reset-password` | authMiddleware | aucun |

## `/stats`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/stats/dashboard/summary` | authMiddleware | `reports.sales.read` |
| GET | `/stats/upsell` | authMiddleware | `pos.analytics` (RBAC lot 10) |

`GET /stats/dashboard/summary` alimente la tuile de reporting de la page
d'accueil back-office (RBAC lot 8). **À traiter côté front** : un rôle sans
`reports.sales.read` reçoit désormais un 403 sur cet appel — le back-office
doit masquer la tuile sur ce 403, pas casser la page d'accueil.

## `/pos`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/pos/create` | authMiddleware | aucun |
| POST | `/pos/link-user` | authMiddleware | `staff.manage` |
| GET | `/pos/status` | authMiddleware | aucun |
| PATCH | `/pos/status` | authMiddleware | `pos.status.manage` |
| GET | `/pos/deletion_reasons/{object}` | authMiddleware | aucun |
| GET | `/pos/delivery_men` | authMiddleware | aucun |
| GET | `/pos/users` | authMiddleware | aucun |
| GET | `/pos/tva_rates` | authMiddleware | aucun |
| GET, PATCH | `/pos/settings/` | authMiddleware | aucun |
| POST | `/pos/settings/logo` | authMiddleware | aucun |
| GET | `/pos/settings/holidays` | authMiddleware | aucun |
| PATCH, DELETE | `/pos/settings/holidays/{date}` | authMiddleware | aucun |
| GET, POST | `/pos/settings/vacations` | authMiddleware | aucun |
| PATCH, DELETE | `/pos/settings/vacations/{id}` | authMiddleware | aucun |
| POST | `/pos/settings/hours_of_operations` | authMiddleware | aucun |
| PATCH, DELETE | `/pos/settings/hours_of_operations/{hour_id}` | authMiddleware | aucun |
| PATCH | `/pos/settings/scannorder` | authMiddleware | aucun |
| PATCH | `/pos/settings/production_paid_only` | authMiddleware | aucun |
| PATCH | `/pos/settings/safety_stock` | authMiddleware | aucun |
| GET | `/pos/payments/tr/check/{tr_code}` | authMiddleware | aucun |
| POST | `/pos/reports/tva` | authMiddleware | `reports.sales.read` |
| POST | `/pos/reports/payments` | authMiddleware | `reports.sales.read` |
| POST | `/pos/reports/tva/export` | authMiddleware | `reports.sales.read` |
| POST | `/pos/reports/payments/export` | authMiddleware | `reports.sales.read` |
| POST | `/pos/accounting/export` | authMiddleware | aucun |

## `/scannorder` — public (commande client final via QR code)

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/scannorder/brands/{brand_slug}` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/slots` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/menu` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/loyalty_programs` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/discounts` | aucune | aucun |
| GET, POST | `/scannorder/{merchant_slug}/upsell` | aucune | aucun |
| POST | `/scannorder/{merchant_slug}/pricing` | aucune | aucun |
| POST | `/scannorder/{merchant_slug}/delivery/check` | aucune | aucun |
| POST | `/scannorder/{merchant_slug}/orders` | aucune | aucun |
| POST | `/scannorder/{merchant_slug}/create` (dépréciée) | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/orders/{order_id}` | aucune | aucun |
| GET | `/scannorder/{merchant_slug}/products/{product_id}` | aucune | aucun |
| DELETE | `/scannorder/{merchant_slug}/orders/{order_id}` | aucune | aucun |

## `/accounting`

Groupe entier derrière `reports.financial.read` (RBAC lot 8).

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/accounting/vat/calculate` | authMiddleware | `reports.financial.read` |
| POST | `/accounting/vat/export-csv` | authMiddleware | `reports.financial.read` |
| POST | `/accounting/registers/{register_id}/export-pdf` | authMiddleware | `reports.financial.read` |

## `/stocks`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/stocks/barcode/{barcode}` | authMiddleware | aucun |
| POST | `/stocks/barcode/create` | authMiddleware | aucun |
| DELETE | `/stocks/barcode/{barcode}` | authMiddleware | aucun |
| POST | `/stocks/barcodes/scan` | authMiddleware | aucun |
| PATCH | `/stocks/loss` | authMiddleware | aucun |
| GET | `/stocks/products` | authMiddleware | aucun |
| GET | `/stocks/components/list` | authMiddleware | aucun |
| PUT | `/stocks/components/{component_id}` | authMiddleware | `inventory.manage` |
| GET | `/stocks/movements` | authMiddleware | aucun |

`PUT /stocks/components/{component_id}` (add/remove/loss d'un composant) est
la seule route gardée par `inventory.manage` (RBAC lot 8) : c'est la
CORRECTION de stock. Les lectures ci-dessus restent libres à dessein —
gager `GET /stocks/*` avec un droit `*.manage` aurait reproduit l'anti-pattern
que ce lot corrige par ailleurs (voir `docs/decisions.md`).

## `/device`, `/app`, `/uploads`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/device/token` | authMiddleware | aucun |
| POST | `/app/version/check` | aucune | aucun |
| POST | `/uploads/haccp` | authMiddleware | aucun |

## `/menu`

Groupe entier sous `authMiddleware`. Seules trois routes (l'import en masse) portent une garde
RBAC — décision documentée dans le fichier (`docs/audit-import-produits.md §1.7`).

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/menu/` | authMiddleware | aucun |
| GET, PATCH | `/menu/translation-langs` | authMiddleware | aucun |
| GET | `/menu/products` | authMiddleware | aucun |
| GET | `/menu/products/allergens/poster.pdf` | authMiddleware | aucun |
| POST | `/menu/import/preview` | authMiddleware | `catalog.manage` |
| POST | `/menu/import/commit` | authMiddleware | `catalog.manage` |
| GET | `/menu/import/template` | authMiddleware | `catalog.manage` |
| GET | `/menu/components` | authMiddleware | aucun |
| GET | `/menu/components/{component_id}` | authMiddleware | aucun |
| PATCH | `/menu/component/{component_id}/status` | authMiddleware | aucun |
| PATCH | `/menu/components/{component_id}` | authMiddleware | aucun |
| DELETE | `/menu/components/{component_id}` | authMiddleware | aucun |
| PATCH | `/menu/display-orders` | authMiddleware | aucun |
| PATCH | `/menu/products/categories/{category_id}` | authMiddleware | aucun |
| PATCH | `/menu/products/categories/{category_id}/availability` | authMiddleware | aucun |
| PUT, DELETE | `/menu/products/categories/{category_id}/image` | authMiddleware | aucun |
| PATCH | `/menu/products/categories/{category_id}/bulk-assign` | authMiddleware | aucun |
| DELETE | `/menu/products/categories/{category_id}` | authMiddleware | aucun |
| PATCH, DELETE | `/menu/products/{product_id}/marketing-category` | authMiddleware | aucun |
| POST | `/menu/products` | authMiddleware | aucun |
| GET | `/menu/products/{product_id}` | authMiddleware | aucun |
| PATCH | `/menu/products/{product_id}` | authMiddleware | aucun |
| PATCH | `/menu/products/{product_id}/attributes` | authMiddleware | aucun |
| PUT | `/menu/products/{product_id}/image` | authMiddleware | aucun |
| PATCH | `/menu/products/{product_id}/status` | authMiddleware | aucun |
| PATCH | `/menu/products/{product_id}/availability` | authMiddleware | aucun |
| DELETE | `/menu/products/{product_id}` | authMiddleware | aucun |
| PUT | `/menu/products/{product_id}/allergens` | authMiddleware | aucun |
| PUT | `/menu/products/{product_id}/tags` | authMiddleware | aucun |
| GET | `/menu/attributes` | authMiddleware | aucun |
| GET | `/menu/attributes/{attribute_id}` | authMiddleware | aucun |
| POST | `/menu/attributes` | authMiddleware | aucun |
| PATCH | `/menu/attributes/{attribute_id}` | authMiddleware | aucun |
| DELETE | `/menu/attributes/{attribute_id}` | authMiddleware | aucun |
| PUT | `/menu/attribute_options/{option_id}/image` | authMiddleware | aucun |
| GET | `/menu/units_of_measures` | authMiddleware | aucun |
| GET | `/menu/tags/` | authMiddleware | aucun |
| POST | `/menu/tags/create` | authMiddleware | aucun |
| PATCH | `/menu/tags/display-order` | authMiddleware | aucun |
| PATCH | `/menu/tags/{tag_id}/bulk_assign` | authMiddleware | aucun |
| PATCH | `/menu/tags/{tag_id}` | authMiddleware | aucun |
| DELETE | `/menu/tags/{tag_id}` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk/status` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk/attributes` | authMiddleware | aucun |
| POST | `/menu/products/bulk/delete` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk/tags` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk/tva` | authMiddleware | aucun |
| PATCH | `/menu/products/bulk/availability` | authMiddleware | aucun |
| POST | `/menu/bulk/allergens/assign` | authMiddleware | aucun |
| POST | `/menu/bulk/tags/assign` | authMiddleware | aucun |
| POST | `/menu/bulk/attributes/assign` | authMiddleware | aucun |
| GET | `/menu/deliveroo` | authMiddleware | aucun |
| PATCH | `/menu/deliveroo/sync` | authMiddleware | aucun |
| GET | `/menu/uber-eats` | authMiddleware | aucun |
| PATCH | `/menu/uber-eats/sync` | authMiddleware | aucun |
| POST | `/menu/products/categories` | authMiddleware | aucun |
| GET, POST | `/menu/marketing-categories` | authMiddleware | aucun |
| PATCH | `/menu/marketing-categories/display-order` | authMiddleware | aucun |
| PATCH | `/menu/marketing-categories/{category_id}` | authMiddleware | aucun |
| PUT, DELETE | `/menu/marketing-categories/{category_id}/image` | authMiddleware | aucun |
| DELETE | `/menu/marketing-categories/{category_id}` | authMiddleware | aucun |
| PATCH | `/menu/marketing-categories/{category_id}/bulk-assign` | authMiddleware | aucun |
| POST | `/menu/components` | authMiddleware | aucun |
| POST | `/menu/components/categories` | authMiddleware | aucun |
| PATCH | `/menu/components/categories/display-order` | authMiddleware | aucun |
| PATCH | `/menu/components/categories/{category_id}` | authMiddleware | aucun |
| DELETE | `/menu/components/categories/{category_id}` | authMiddleware | aucun |
| GET | `/menu/discounts` | authMiddleware | aucun |
| GET | `/menu/discounts/all` | authMiddleware | aucun |
| POST | `/menu/discounts` | authMiddleware | aucun |
| GET | `/menu/discounts/{discount_id}` | authMiddleware | aucun |
| PATCH | `/menu/discounts/{discount_id}` | authMiddleware | aucun |
| DELETE | `/menu/discounts/{discount_id}` | authMiddleware | aucun |
| GET | `/menu/availabilities` | authMiddleware | aucun |
| POST | `/menu/availabilities` | authMiddleware | aucun |
| PATCH | `/menu/availabilities/{id}` | authMiddleware | aucun |
| DELETE | `/menu/availabilities/{id}` | authMiddleware | aucun |
| GET | `/menu/availabilities/check` | authMiddleware | aucun |

## `/haccp`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/haccp/settings` | authMiddleware | aucun |
| PUT | `/haccp/settings` | authMiddleware | `haccp.manage` |
| GET | `/haccp/hub` | authMiddleware | aucun |
| GET | `/haccp/activities` | authMiddleware | aucun |
| GET | `/haccp/temperature-zones` | authMiddleware | aucun |
| GET | `/haccp/corrective-actions` | authMiddleware | aucun |
| POST | `/haccp/temperature-zones` | authMiddleware | aucun |
| PATCH, DELETE | `/haccp/temperature-zones/{id}` | authMiddleware | aucun |
| GET | `/haccp/temperature-readings` | authMiddleware | aucun |
| POST | `/haccp/temperature-readings/batch` | authMiddleware | aucun |
| GET | `/haccp/temperature-sessions/{id}` | authMiddleware | aucun |
| GET, POST | `/haccp/cleaning-zones` | authMiddleware | aucun |
| PATCH, DELETE | `/haccp/cleaning-zones/{id}` | authMiddleware | aucun |
| GET, POST | `/haccp/cleaning-surfaces` | authMiddleware | aucun |
| PATCH, DELETE | `/haccp/cleaning-surfaces/{id}` | authMiddleware | aucun |
| GET, POST | `/haccp/cleaning-sessions` | authMiddleware | aucun |
| GET | `/haccp/cleaning-sessions/{id}` | authMiddleware | aucun |
| POST | `/haccp/goods-receipts` | authMiddleware | aucun |
| GET | `/haccp/components` | authMiddleware | aucun |
| POST, GET | `/haccp/traceability/` | authMiddleware | aucun |
| GET | `/haccp/traceability/{id}` | authMiddleware | aucun |

`haccp.manage` gardait ces trois routes jusqu'au RBAC lot 8 (2026-08-27), qui a
retiré la garde : la traçabilité est une SAISIE opérationnelle quotidienne
(réception de marchandise tracée), au même titre que le relevé de température
ou le log de nettoyage juste au-dessus — tous libres. Motif d'origine (posé en
juillet 2026, jamais documenté par écrit avant ce lot) et confirmation du
retrait : voir `docs/decisions.md`.

**Suite donnée à l'orphelinage** : plutôt que de sortir `haccp.manage` du
catalogue, il a été reposé sur `PUT /haccp/settings` (paramétrer les seuils et
réglages du module — CONFIGURATION, à l'inverse de la traçabilité ci-dessus
qui est de la SAISIE). `TestRBACPermissionCoverage` passe de nouveau. Les
autres candidats CONFIGURATION identifiés lors de l'audit (créer/éditer les
zones et surfaces de nettoyage, les zones de température — voir
`docs/RBAC_CLIENTS.md` §5) restent libres ; ce lot ne les a pas traités.

**À faire dans un lot futur (non traité ici)** : masquer la section HACCP du
menu back-office pour tout compte sans `haccp.manage`, symétriquement à ce que
la garde API fait déjà sur `PUT /haccp/settings`. Aujourd'hui rien côté
back-office ne cache ce menu — un compte sans le droit verrait l'écran de
paramétrage HACCP puis un 403 à l'enregistrement. Demande explicitement
différée à l'implémentation ; seule cette note constitue le suivi écrit pour
l'instant.

## `/planning/me` — libre-service (authMiddleware seul, pas de garde de groupe)

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/planning/me/time-entries` | authMiddleware | aucun |
| GET | `/planning/me/time-entries/current` | authMiddleware | aucun |
| POST | `/planning/me/time-entries/start` | authMiddleware | aucun |
| POST | `/planning/me/time-entries/stop` | authMiddleware | aucun |
| GET | `/planning/me/team-week` | authMiddleware | aucun |
| GET, POST | `/planning/me/leave-requests` | authMiddleware | aucun |
| GET, POST | `/planning/me/shift-swap-requests` | authMiddleware | aucun |
| POST | `/planning/me/shift-swap-requests/{id}/accept` | authMiddleware | aucun |
| POST | `/planning/me/shift-swap-requests/{id}/reject` | authMiddleware | aucun |

## `/planning` — garde de groupe partielle (`r.Use` sur un sous-groupe)

RBAC lot 8 (audit lot 7 §4.3) : la garde `staff.schedule.manage`, posée en tête
de tout le groupe jusque-là, est retirée et reposée sur un sous-groupe
couvrant tout `/planning` **sauf** les 4 lectures de référentiel ci-dessous
(labels/couleurs, pas de donnée RH ou salariale — vérifié dans le code Go),
qui passent libres.

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET, PUT | `/planning/settings` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/contract-types` | authMiddleware | aucun |
| GET | `/planning/attendance-sources` | authMiddleware | aucun |
| GET | `/planning/event-types` | authMiddleware | aucun |
| GET | `/planning/positions` | authMiddleware | aucun |
| POST | `/planning/positions` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/positions/{id}` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/shift-templates` | authMiddleware | `staff.schedule.manage` |
| PATCH, DELETE | `/planning/shift-templates/{id}` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/week-templates` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/week-templates/{id}` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/week-templates` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/week-templates/from-week` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/week-templates/{id}/preview` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/week-templates/{id}/instantiate` | authMiddleware | `staff.schedule.manage` |
| PATCH, DELETE | `/planning/week-templates/{id}` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/employees` | authMiddleware | `staff.schedule.manage` |
| PATCH | `/planning/employees/display-order` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/employees/{id}` | authMiddleware | `staff.schedule.manage` |
| POST, DELETE | `/planning/employees/{id}/user-link` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/employees/{id}/documents` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/employees/{id}/time-entries` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/employees/{id}/time-entries/current` | authMiddleware | `staff.schedule.manage` |
| PATCH, DELETE | `/planning/employees/{id}/time-entries/{entry_id}` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/employees/{id}/time-entries/start` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/employees/{id}/time-entries/stop` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/uploads/employee-documents` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/employees/{id}/documents/{document_id}/download` | authMiddleware | `staff.schedule.manage` |
| DELETE | `/planning/employees/{id}/documents/{document_id}` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/weeks` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/weeks/{id}` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/weeks/{id}/publish` | authMiddleware | `staff.schedule.manage` |
| POST | `/planning/weeks/{id}/unpublish` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/weeks/{id}/shifts` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/shifts` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/shifts/{id}` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/day-comments` | authMiddleware | `staff.schedule.manage` |
| PUT, DELETE | `/planning/day-comments/{date}` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/leave-requests` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/leave-requests/{id}` | authMiddleware | `staff.schedule.manage` |
| GET | `/planning/leave-requests/{id}/conflicting-shifts` | authMiddleware | `staff.schedule.manage` |
| GET, POST | `/planning/shift-swap-requests` | authMiddleware | `staff.schedule.manage` |
| GET, PATCH, DELETE | `/planning/shift-swap-requests/{id}` | authMiddleware | `staff.schedule.manage` |
| PUT | `/planning/revenue-forecast` | authMiddleware | `staff.schedule.manage` |

`GET /planning/performance` (coût de la main d'œuvre vs CA) est sortie du
sous-groupe ci-dessus : ce n'est pas un geste d'encadrement du planning mais
un rapport financier — voir ci-dessous.

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/planning/performance` | authMiddleware | `reports.financial.read` |

## `/allergens`, `/printers`, `/floors`, `/locations`, `/services`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/allergens/` | authMiddleware | aucun |
| GET, POST | `/printers/` | authMiddleware | aucun |
| PATCH, DELETE | `/printers/{printer_id}` | authMiddleware | aucun |
| POST | `/floors/` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| PATCH, DELETE | `/floors/{floor_id}` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| POST | `/floors/{floor_id}/obstacles/` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| PATCH, DELETE | `/floors/{floor_id}/obstacles/{obstacle_id}` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| POST | `/floors/{floor_id}/areas/` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| PATCH, DELETE | `/floors/{floor_id}/areas/{area_id}` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| GET | `/locations/` | authMiddleware | aucun (utilisé aussi par la prise de commande) |
| POST | `/locations/floors/{floor_id}/tables` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| PATCH, DELETE | `/locations/tables/{location_id}` | authMiddleware | `seating_plan.manage` (RBAC lot 10) |
| GET | `/services/{device_id}` | authMiddleware | aucun |

## `/orders`

Le sous-groupe d'écriture était gardé par `IsEmailVerified` avant ce lot (voir résumé de
livraison) — retiré à la bascule, impact réel nul (toujours `true`). RBAC lot 8 y a posé
deux gardes ciblées (`pos.ticket.reopen`, `pos.refund`) ; le reste n'a toujours pas de garde
RBAC — `POST /orders/history` en particulier reste délibérément libre : consultation
opérationnelle quotidienne (écran d'historique de l'app Flutter), pas du reporting.

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/orders/pricing` | authMiddleware | aucun |
| POST | `/orders/upsell` | authMiddleware | aucun |
| POST | `/orders/list` | authMiddleware | aucun |
| GET | `/orders/pending` | authMiddleware | aucun |
| POST | `/orders/history` | authMiddleware | aucun |
| GET | `/orders/{order_id}` | authMiddleware | aucun |
| GET | `/orders/{order_id}/payments` | authMiddleware | aucun |
| POST | `/orders/create` | authMiddleware | aucun |
| POST | `/orders/{order_id}/update` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/reopen` | authMiddleware | `pos.ticket.reopen` |
| POST | `/orders/{order_id}/refund` | authMiddleware | `pos.refund` |
| POST | `/orders/{order_id}/invoice/email-sms` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/accept` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/deny` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/cancel` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/delivered` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/delivery-start` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/distributed` | authMiddleware | aucun |
| PATCH | `/orders/{order_id}/distributed-products` | authMiddleware | aucun |
| PATCH | `/orders/multiple-production-status` | authMiddleware | aucun |
| POST | `/orders/{order_id}/payments/create` | authMiddleware | aucun |
| DELETE | `/orders/{order_id}/payments/{payment_id}` | authMiddleware | `pos.refund` |

## `/admin/upsell`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/admin/upsell/recompute-patterns` | authMiddleware | aucun (TODO explicite dans le code : middleware admin dédié à venir) |

## `/delivery_sessions`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/delivery_sessions/pending` | authMiddleware | aucun |
| GET | `/delivery_sessions/me` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/stops/{order_id}/select` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/stops/{order_id}/arrived` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/stops/{order_id}/delivered` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/stops/{order_id}/failed` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/stops/{order_id}/cancel` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/me/close` | authMiddleware | aucun |
| GET | `/delivery_sessions/{delivery_session_id}` | authMiddleware | aucun |
| DELETE | `/delivery_sessions/{delivery_session_id}` | authMiddleware | aucun |
| PATCH | `/delivery_sessions/{delivery_session_id}/close` | authMiddleware | aucun |
| POST | `/delivery_sessions/start` | authMiddleware | aucun |

## `/cash_drawer`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/cash_drawer/open` | authMiddleware | `pos.cash_drawer.open` |

RBAC lot 8 : `GET` → `POST` en plus de la garde — un endpoint dont l'objet est
de déclencher un effet de bord physique (ouvrir le tiroir-caisse hors
encaissement) n'est pas une lecture. **Changement de contrat d'API** : tout
client appelant encore en `GET` doit migrer vers `POST` (voir
`docs/decisions.md`).

## `/customer` (dépréciée depuis 2024-06-25) et `/customers`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/customer/search` | authMiddleware | aucun |
| GET | `/customer/list` | authMiddleware | aucun |
| GET, POST | `/customer/loyalty-programs` | authMiddleware | aucun |
| GET, PATCH, DELETE | `/customer/loyalty-programs/{loyalty_program_id}` | authMiddleware | aucun |
| GET | `/customer/{customer_id}/loyalty` | authMiddleware | aucun |
| PATCH | `/customer/{customer_id}/loyalty/{loyalty_program_id}` | authMiddleware | aucun |
| PATCH | `/customer/{customer_id}/rewards/{reward_id}` | authMiddleware | aucun |
| POST | `/customers/import/preview` | authMiddleware | `customers.manage` |
| POST | `/customers/import/commit` | authMiddleware | `customers.manage` |
| GET | `/customers/import/template` | authMiddleware | `customers.manage` |
| POST | `/customers/` | authMiddleware | aucun |
| GET | `/customers/search` | authMiddleware | aucun |
| GET | `/customers/list` | authMiddleware | aucun |
| GET, POST | `/customers/loyalty-programs` | authMiddleware | aucun |
| GET, PATCH, DELETE | `/customers/loyalty-programs/{loyalty_program_id}` | authMiddleware | aucun |
| GET | `/customers/{customer_id}/loyalty` | authMiddleware | aucun |
| PATCH | `/customers/{customer_id}/loyalty/{loyalty_program_id}` | authMiddleware | aucun |
| PATCH | `/customers/{customer_id}/rewards/{reward_id}` | authMiddleware | aucun |

## `/cash_register`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/cash_register/open` | authMiddleware | aucun |
| GET, POST | `/cash_register/history` | authMiddleware | aucun |
| POST | `/cash_register/link` | authMiddleware | aucun |
| DELETE | `/cash_register/link` | authMiddleware | aucun |
| GET | `/cash_register/{cash_register_id}/` | authMiddleware | aucun |
| GET | `/cash_register/{cash_register_id}/summary` | authMiddleware | aucun |
| GET | `/cash_register/{cash_register_id}/tva-details` | authMiddleware | aucun |
| PATCH | `/cash_register/{cash_register_id}/close` | authMiddleware | aucun |
| PATCH | `/cash_register/{cash_register_id}/enclose` | authMiddleware | aucun |
| POST | `/cash_register/{cash_register_id}/custom_items` | authMiddleware | aucun |
| DELETE | `/cash_register/{cash_register_id}/custom_items/{item_id}` | authMiddleware | aucun |

## `/bookings`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET, POST | `/bookings/` | authMiddleware | aucun |
| GET | `/bookings/availability/{date}` | authMiddleware | aucun |
| GET | `/bookings/settings` | authMiddleware | aucun |
| PUT | `/bookings/settings` | authMiddleware | `bookings.manage` (RBAC lot 10) |
| GET | `/bookings/settings/duration-rules` | authMiddleware | aucun |
| POST | `/bookings/settings/duration-rules` | authMiddleware | `bookings.manage` (RBAC lot 10) |
| PATCH, DELETE | `/bookings/settings/duration-rules/{rule_id}` | authMiddleware | `bookings.manage` (RBAC lot 10) |
| GET | `/bookings/settings/hours` | authMiddleware | aucun |
| PUT | `/bookings/settings/hours` | authMiddleware | `bookings.manage` (RBAC lot 10) |
| POST | `/bookings/create` | authMiddleware | aucun |
| GET | `/bookings/{booking_id}` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/accept` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/deny` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/cancel` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/reschedule` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/seat` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/complete` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/no-show` | authMiddleware | aucun |
| PATCH | `/bookings/{booking_id}/locations` | authMiddleware | aucun |
| GET, POST | `/bookings/waitlist` | authMiddleware | aucun |
| PATCH | `/bookings/waitlist/{id}/seat` | authMiddleware | aucun |
| PATCH | `/bookings/waitlist/{id}/cancel` | authMiddleware | aucun |
| DELETE | `/bookings/waitlist/{id}` | authMiddleware | aucun |

## `/rsv/{slug}` — public, réservation client (rate-limited, pas de jeton)

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/rsv/{slug}/open-hours` | rate-limit IP (60/min) | aucun |
| GET | `/rsv/{slug}/booking-availability` | rate-limit IP (60/min) | aucun |
| POST | `/rsv/{slug}/booking/create` | rate-limit IP (10/min) | aucun |
| GET | `/rsv/{slug}/booking/{booking_id}` | rate-limit IP (10/min) | aucun |
| DELETE | `/rsv/{slug}/booking/{booking_id}/cancel` | rate-limit IP (10/min) | aucun |
| POST | `/rsv/{slug}/booking/{booking_id}/update` | rate-limit IP (10/min) | aucun |
| POST | `/rsv/{slug}/waitlist` | rate-limit IP (10/min) | aucun |
| GET | `/rsv/{slug}/waitlist/{waitlist_token}` | rate-limit IP (10/min) | aucun |
| DELETE | `/rsv/{slug}/waitlist/{waitlist_token}` | rate-limit IP (10/min) | aucun |

## `/integrations`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/integrations/uber-eats/connect` | aucune (OAuth) | aucun |
| GET | `/integrations/uber-eats/callback` | aucune (OAuth) | aucun |
| GET | `/integrations/uber-eats/disconnect` | aucune (OAuth) | aucun |
| GET | `/integrations/uber-eats` | authMiddleware | aucun |
| GET | `/integrations/deliveroo` | authMiddleware | aucun |
| GET | `/integrations/scannorder` | authMiddleware | aucun |
| PUT | `/integrations/scannorder/logo` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PUT | `/integrations/scannorder/banner` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/uber-eats` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/uber-eats/disable` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/deliveroo` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/deliveroo/disable` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/scannorder` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| POST | `/integrations/scannorder/onboarding` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/global/close-temporary` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| PATCH | `/integrations/global/wait-time` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| GET | `/integrations/stripe/status` | authMiddleware | aucun |
| POST | `/integrations/stripe/onboarding-link` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| GET | `/integrations/stripe/bank-accounts` | authMiddleware | aucun |
| POST | `/integrations/stripe/bank-account-link` | authMiddleware | `platforms.manage` (RBAC lot 10) |
| GET | `/integrations/stripe/balance` | authMiddleware | `reports.financial.read` |
| POST | `/integrations/stripe/branding` | authMiddleware | `platforms.manage` (RBAC lot 10) |

## `/kiosk` — device, `KioskAuth` (pas `authMiddleware`)

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/kiosk/auth/enroll` | aucune (enrôlement initial) | aucun |
| POST | `/kiosk/auth/token/refresh` | aucune (refresh token device) | aucun |
| POST | `/kiosk/auth/reclaim` | aucune | aucun |
| POST | `/kiosk/auth/heartbeat` | KioskAuth | aucun |
| POST | `/kiosk/auth/verify-admin-pin` | KioskAuth | aucun |
| GET | `/kiosk/menu` | KioskAuth | aucun |
| GET | `/kiosk/products/{product_id}` | KioskAuth | aucun |
| GET | `/kiosk/settings` | KioskAuth | aucun |
| GET | `/kiosk/discounts` | KioskAuth | aucun |
| POST | `/kiosk/upsell` | KioskAuth | aucun |
| POST | `/kiosk/pricing` | KioskAuth | aucun |
| POST | `/kiosk/orders` | KioskAuth | aucun |
| GET | `/kiosk/orders/{order_id}` | KioskAuth | aucun |
| DELETE | `/kiosk/orders/{order_id}` | KioskAuth | aucun |
| POST | `/kiosk/orders/{order_id}/counter-payment` | KioskAuth | aucun |
| POST | `/kiosk/orders/{order_id}/switch-to-counter-payment` | KioskAuth | aucun |
| POST | `/kiosk/status/unavailable` | KioskAuth | aucun |
| POST | `/kiosk/terminal/connection-token` | KioskAuth | aucun |
| POST | `/kiosk/terminal/payment-intent` | KioskAuth | aucun |
| POST | `/kiosk/terminal/payment-intent/{payment_intent_id}/cancel` | KioskAuth | aucun |

## `/pos/kiosk`, `/pos/settings/kiosk` — administration back-office des bornes

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| POST | `/pos/kiosk/{kiosk_id}/status` | authMiddleware | aucun |
| POST | `/pos/settings/kiosk/enrollment-codes` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| GET | `/pos/settings/kiosk/enrollment-codes` | authMiddleware | aucun |
| DELETE | `/pos/settings/kiosk/enrollment-codes/{code_id}` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| GET | `/pos/settings/kiosk/devices` | authMiddleware | aucun |
| GET | `/pos/settings/kiosk/devices/{device_id}` | authMiddleware | aucun |
| PUT | `/pos/settings/kiosk/devices/{device_id}` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/devices/{device_id}/enable` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/devices/{device_id}/disable` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/devices/{device_id}/revoke` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| GET | `/pos/settings/kiosk/devices/{device_id}/admin-pin` | authMiddleware | `settings.manage` |
| POST | `/pos/settings/kiosk/devices/{device_id}/regenerate-admin-pin` | authMiddleware | `settings.manage` |
| GET | `/pos/settings/kiosk/settings` | authMiddleware | aucun |
| PUT | `/pos/settings/kiosk/settings` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/settings/logo` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/settings/idle-image` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| POST | `/pos/settings/kiosk/settings/idle-video` | authMiddleware | `kiosk.manage` (RBAC lot 10) |
| DELETE | `/pos/settings/kiosk/settings/idle-video` | authMiddleware | `kiosk.manage` (RBAC lot 10) |

## `/ws`, `/ws-kiosk`

| Méthode | Route | Auth | Droit requis |
|---|---|---|---|
| GET | `/ws/` | authMiddleware | aucun |
| GET | `/ws-kiosk/` | KioskAuth | aucun |

---

## Résumé chiffré

Les comptes routes-par-droit ci-dessous (`staff.manage` ×10, `staff.schedule.manage`
×43, etc.) dataient de RBAC lot 2 et n'ont pas été ré-audités lot par lot depuis —
seuls les chiffres touchés par RBAC lot 8 ci-dessous sont à jour. Le principe
directeur reste inchangé : ce document rend visible l'état réel du routeur, il
ne prescrit pas quelles routes devraient être gardées au-delà de ce que chaque
lot a explicitement décidé.

### RBAC lot 8 (2026-08-27) — catalogue 15 → 13, huit droits orphelins traités (+ un neuvième né en cours de lot)

- **Catalogue réduit à 13 droits** : `pos.access` et `pos.discount.apply`
  supprimés (aucune route ne les gardait, aucune n'est prévue). Voir
  `docs/decisions.md`.
- **Trois gardes mal posées retirées** : `customers.manage` sur `POST
  /customers/` (création unitaire — SAISIE, pas CONFIGURATION), la garde de
  groupe `staff.schedule.manage` sur 4 lectures de référentiel `/planning`
  (`contract-types`, `attendance-sources`, `event-types`, `positions`), et
  `haccp.manage` sur tout `/haccp/traceability` (SAISIE opérationnelle
  quotidienne, comme le reste du module HACCP).
- **Sept droits orphelins reliés à une route** : `pos.ticket.reopen` (`PATCH
  /orders/{id}/reopen`), `pos.refund` (`POST /orders/{id}/refund`, `DELETE
  /orders/{id}/payments/{payment_id}`), `pos.cash_drawer.open` (`POST
  /cash_drawer/open` — devenu `POST`, était `GET`), `inventory.manage` (`PUT
  /stocks/components/{id}` uniquement — pas les lectures `/stocks/*`),
  `reports.sales.read` (`/pos/reports/*`, `GET /stats/dashboard/summary`),
  `reports.financial.read` (`/accounting/*`, `GET
  /integrations/stripe/balance`, `GET /planning/performance` — déplacée hors
  de `staff.schedule.manage`), et `haccp.manage`, orphelin le temps de ce lot
  (conséquence du retrait de sa garde sur `/haccp/traceability` ci-dessus),
  reposé sur `PUT /haccp/settings` — voir la section `/haccp`.
- **Suivi différé, non implémenté ici** : masquer la section HACCP du menu
  back-office pour un compte sans `haccp.manage` — voir la note dans la
  section `/haccp`.
- **Ratchet des routes mutatives non gardées** (`cmd/api/routes_rbac_ratchet_test.go`) :
  222 → **212**.
- **Rôle système « Employé polyvalent » (`staff`) : ne porte plus aucun droit
  par défaut.** C'était le seul rôle à porter `pos.access` et
  `pos.discount.apply` — les deux droits supprimés ci-dessus. Résultat
  attendu, pas une régression à corriger : tout ce qu'un employé polyvalent
  fait au quotidien (encaisser, appliquer une remise, etc.) reste
  intégralement libre côté routes ; les 13 droits restants au catalogue
  gardent tous des gestes d'encadrement (correction, configuration, rapport)
  qu'un employé polyvalent n'exerce pas. Voir `docs/decisions.md`.
- **Nouveau test de non-régression** : `TestRBACPermissionCoverage`
  (`cmd/api/routes_rbac_permission_coverage_test.go`) échoue si un droit du
  catalogue (`permission.All`) n'est référencé par aucun
  `middleware.RequirePermission(...)` dans `cmd/api/routes.go` — le pendant
  du ratchet, côté catalogue plutôt que côté routeur. Il a effectivement
  attrapé l'orphelinage de `haccp.manage` en cours de lot (voir ci-dessus) ;
  les 13 droits du catalogue passent tous à la fin de ce lot.
