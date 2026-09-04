# Le repli historique de `Has()` — état exact et condition de retrait

RBAC lot 11, phase 5. Ce document décrit précisément la seconde branche de
`UserLoginRow.Has()` (`internal/modules/auth/permissions.go`) — celle qui
reste active quand `RoleID == nil` — pour que personne n'ait à relire le code
pour savoir quelle clé est couverte, ce qui se passe pour celles qui ne le
sont pas, et à quelle condition cette branche pourra un jour disparaître.

**Cette branche n'est pas retirée par le chantier RBAC lot 11** (« le rôle
administrateur devient un rôle comme les autres » — voir `docs/decisions.md`,
phases 1 à 4). C'est le code qui fait tourner la production aujourd'hui : les
migrations RBAC (094 à 103, 110) n'y ont jamais été appliquées (voir
`docs/RBAC_BASCULE.md`), donc `users_rights.role_id` y est `NULL` pour tous
les comptes, sans exception connue.

## Le code exact

```go
// Monde historique : admin court-circuite tout, comme aujourd'hui.
if u.Rights.Admin {
    return true
}
if fallback, ok := legacyPermissionFallback[key]; ok {
    return fallback(u.Rights)
}
return false
```

Deux choses à retenir avant le tableau :

1. **`Rights.Admin` (la colonne `users_rights.admin`) court-circuite tout,
   avant même de consulter la table de repli.** Un compte avec `admin = true`
   obtient toutes les clés du catalogue, y compris celles qui n'ont aucune
   entrée de repli. C'est le même court-circuit historique que celui retiré
   du monde *rôle* en phase 3 — il reste ici, dans le monde `RoleID == nil`,
   volontairement.
2. **Une clé sans entrée dans `legacyPermissionFallback` n'est donc
   accessible, en mode historique, qu'aux comptes `admin = true`.** Pas de
   valeur par défaut « accordé », pas d'erreur — `Has()` renvoie simplement
   `false`.

## Tableau des 18 clés du catalogue

| Clé | Repli historique | Champ `UserRowRights` | Note |
|---|---|---|---|
| `pos.status.manage` | Oui | `AccessReception` | |
| `pos.cash_drawer.open` | Oui | `OpenCashDrawer` | |
| `catalog.manage` | Oui | `CanManageMenu` | |
| `haccp.manage` | Oui | `CanManageHACCP` | |
| `customers.manage` | Oui | `CanManageCustomers` | |
| `staff.manage` | Oui | `CanManageUsers` | |
| `staff.schedule.manage` | Oui | `CanManagePlannings` | |
| `reports.sales.read` | **Oui** | `CanViewReports` | Vérifié explicitement pour ce chantier — voir §"reports.sales.read" plus bas |
| `reports.financial.read` | Oui | `CanViewFinancials` | |
| `settings.manage` | Oui | `CanManageSettings` | |
| `pos.ticket.reopen` | Non | — | Jamais eu de booléen dédié ; historiquement réservé à `Rights.Admin` seul (voir le commentaire de `legacyPermissionFallback` dans le code) |
| `pos.refund` | Non | — | Idem |
| `inventory.manage` | Non | — | Idem |
| `bookings.manage` | Non | — | RBAC lot 10 (migration 103) — clé créée après la fin de l'ajout de colonnes booléennes ; aucun équivalent historique n'a jamais existé |
| `platforms.manage` | Non | — | RBAC lot 10, idem |
| `kiosk.manage` | Non | — | RBAC lot 10, idem |
| `pos.analytics` | Non | — | RBAC lot 10, idem |
| `seating_plan.manage` | Non | — | RBAC lot 10, idem |

**10 clés sur 18 ont un repli. 8 n'en ont pas** : les 3 clés « admin
seulement » historiques (`pos.ticket.reopen`, `pos.refund`,
`inventory.manage`) et les 5 clés du lot 10 (`bookings.manage`,
`platforms.manage`, `kiosk.manage`, `pos.analytics`, `seating_plan.manage`),
ajoutées au catalogue après que la table de repli a cessé de recevoir de
nouvelles entrées.

## `reports.sales.read`

Vérifiée explicitement pour ce chantier, car c'est la clé qui garde les
nouveaux endpoints analytiques (risque de mise en production immédiat si elle
en manquait — un 403 pour tout le monde en production alors que tout
fonctionne en staging) : **`reports.sales.read` a bien une entrée de repli**,
`CanViewReports` (`internal/modules/auth/permissions.go`, ligne
`permission.ReportsSalesRead: func(r UserRowRights) bool { return r.CanViewReports }`).
Un compte de production avec `can_view_reports = true` (ou `admin = true`)
passe cette garde exactement comme avant ce chantier — rien n'y change.

## Ce qui se passe concrètement en production aujourd'hui pour les 8 clés sans repli

Tant que `users_rights.role_id` reste `NULL` partout en production (état
actuel confirmé, voir `docs/RBAC_BASCULE.md`), une route gardée par l'une de
ces 8 clés n'est accessible qu'aux comptes `admin = true` :

- `pos.ticket.reopen`, `pos.refund`, `inventory.manage` : comportement
  **inchangé** par rapport à avant l'introduction du catalogue RBAC — ces
  trois gestes ont toujours été réservés à l'administrateur historique, la
  garde RBAC ne fait que formaliser ce qui existait déjà.
- `bookings.manage`, `platforms.manage`, `kiosk.manage`, `pos.analytics`,
  `seating_plan.manage` (routes listées dans `docs/decisions.md`, entrée RBAC
  lot 10) : **effet de bord non préexistant à documenter**. Avant le lot 10,
  ces routes de configuration (paramètres de réservation, onglet
  Plateformes, gestion Kiosk, `GET /stats/upsell`, plan de salle) n'avaient
  *aucune* garde RBAC — accessibles à tout compte authentifié. Depuis le lot
  10, en l'absence de repli, elles sont de facto retombées à « admin
  historique uniquement » pour tout établissement production tant que son
  `role_id` reste NULL — un resserrement de fait, pas juste une
  formalisation. Si ce n'est pas le comportement voulu en production avant le
  déploiement complet des rôles, c'est un point à trancher séparément (hors
  périmètre de ce chantier) : soit l'accepter comme un resserrement délibéré
  et provisoire, soit ajouter une entrée de repli permissive le temps du
  rollout.

## À quelle condition exacte cette branche peut être supprimée

Formellement : quand la requête suivante renvoie 0, **dans tous les
environnements qui comptent, production comprise** (pas seulement staging —
c'est explicitement la même requête que `docs/RBAC_BASCULE.md` §5 utilise
pour vérifier la bascule) :

```sql
SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL;
```

Tant que cette requête renvoie une valeur non nulle quelque part, au moins un
compte actif dépend encore de cette branche pour obtenir un droit — la
retirer le priverait silencieusement de tout accès RBAC (retour à `false`
pour toute clé, y compris celles qu'il avait par un booléen historique
`true`). La condition n'est pas seulement « migrations RBAC appliquées » :
c'est `role_id` effectivement renseigné sur *chaque ligne active*, ce que
`docs/RBAC_DEPLOIEMENT_PROD.md` (phase 6) détaille comme une étape distincte
et postérieure aux migrations elles-mêmes.

Une fois cette condition vérifiée en production : `legacyPermissionFallback`,
la branche `RoleID == nil` de `Has()`, `HasAdminRole()`'s branche équivalente,
et la colonne `users_rights.admin` elle-même
(`migrations/todo/113_drop_users_rights_admin_column.up.sql`, préparée et non
appliquée — voir `docs/decisions.md`, RBAC lot 11 phase 4) peuvent être
retirés dans cet ordre : code d'abord (un déploiement qui ne lit plus jamais
la colonne), puis seulement la migration de suppression — jamais l'inverse
(principe expansion / déploiement / contraction, voir
`docs/RBAC_DEPLOIEMENT_PROD.md`).
