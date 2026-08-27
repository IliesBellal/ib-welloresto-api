# RBAC lot 6 — API d'administration des rôles

Date : 2026-08-27 · Branche : `staging`

Contrat exact de chaque endpoint (corps de requête, corps de réponse, codes
d'erreur) pour que le lot front puisse être construit sans relire le code Go.
Implémentation : [internal/modules/roles/](../internal/modules/roles/)
(`models.go`, `repository.go`, `service.go`, `handler.go`). Câblage des
routes : [cmd/api/routes.go](../cmd/api/routes.go).

## Conventions générales

- Pas de préfixe de version sur les routes (le dépôt n'en a pas).
- Toute réponse est enveloppée par `models.SendJSON` :
  `{"id": "<module>.<fonction>", "data": <corps ci-dessous>}`. Le champ `id`
  n'est pas documenté endpoint par endpoint ci-dessous (il vaut
  `"roles.<nom_de_fonction>"`, ex. `"roles.create"`, `"roles.list"`) — seul
  `data` l'est.
- Toute erreur passe par `models.SendErrorJSON`, forme
  `{"status": "<code>", "message": "<texte>", "error": "<code>"}` (les trois
  champs portent le même code sauf mention contraire), avec deux exceptions
  enrichies : le conflit de version et l'archivage refusé pour porteurs (voir
  §Erreurs communes).
- Tous les endpoints sont scopés à `currentUser.MerchantID` (jamais un
  identifiant fourni par le client). Un `roleID` d'un autre établissement
  répond **404** (`role_not_found`), jamais 403.
- Groupes de routes et garde : `authMiddleware` puis, sauf mention contraire,
  `middleware.RequirePermission(permission.StaffManage)`.

## Erreurs communes

| status/error | HTTP | Quand |
|---|---|---|
| `role_not_found` | 404 | id inconnu, archivé (pour certaines opérations), ou d'un autre établissement |
| `role_name_required` | 400 | nom vide/absent à la création ou au renommage |
| `role_version_required` | 400 | `version` absent ou ≤ 0 sur un PATCH/PUT verrouillé |
| `role_permission_key_unknown` | 400 | une clé de `permission_keys` n'existe pas au catalogue |
| `role_immutable` | 409 | **G4** — opération sur le rôle `system_key = 'admin'` |
| `role_self_modification` | 409 | **G1** — modifier son propre rôle, ou les droits d'un rôle qu'on porte |
| `role_staff_manage_required` | 409 | **G2** — l'opération viderait l'établissement de tout porteur actif de `staff.manage` |
| `role_is_merchant_default` | 409 | **G6** — archivage du rôle `staff` alors qu'il est `merchant.default_role_id` |
| `merchant_user_not_found` | 404 | `PUT /users/{id}/role` sur un utilisateur/lien absent ou désactivé |
| `invalid_request_body` | 400 | JSON illisible |
| `unauthorized` | 401 | pas de session valide |

**Conflit de version** (**G2 optimiste**, `PATCH /roles/{id}` et
`PUT /roles/{id}/permissions`) — 409, forme enrichie :

```json
{
  "status": "version_conflict",
  "message": "This role was changed by someone else. Reload it and try again.",
  "error": "version_conflict",
  "current_version": 4
}
```

**Rôle encore porté** (**G5**, `POST /roles/{id}/archive`) — 409, forme
enrichie :

```json
{
  "status": "role_has_members",
  "message": "This role is still held by at least one user and cannot be archived.",
  "error": "role_has_members",
  "holder_count": 3
}
```

## Objets

**Permission**

```json
{
  "key": "pos.access",
  "domain": "pos",
  "label": "Encaisser au point de vente",
  "description": "",
  "is_sensitive": false,
  "sort_order": 10,
  "deprecated_at": null
}
```

**Role** (sans les droits — utilisé par la liste)

```json
{
  "id": "role-3f2a...",
  "merchant_id": "12",
  "name": "Chef de cuisine",
  "description": "",
  "system_key": null,
  "version": 1,
  "created_at": "2026-08-27T10:00:00Z",
  "updated_at": "2026-08-27T10:00:00Z",
  "archived_at": null
}
```

`system_key` vaut `"admin"`, `"staff"`, ou est absent/`null` pour un rôle
personnalisé.

**RoleWithPermissions** = Role + `"permissions": [Permission, ...]`.

---

## GET /permissions

Authentifié uniquement (pas de `staff.manage`).

**Réponse 200**

```json
{
  "status": "success",
  "domains": [
    {
      "domain": "pos",
      "permissions": [ Permission, Permission, ... ]
    },
    { "domain": "catalog", "permissions": [ ... ] },
    { "domain": "inventory", "permissions": [ ... ] },
    { "domain": "haccp", "permissions": [ ... ] },
    { "domain": "customers", "permissions": [ ... ] },
    { "domain": "staff", "permissions": [ ... ] },
    { "domain": "reports", "permissions": [ ... ] },
    { "domain": "settings", "permissions": [ ... ] }
  ]
}
```

Les 15 droits du catalogue, groupés par domaine, dans l'ordre de
`sort_order`.

---

## GET /me/permissions

Authentifié uniquement.

**Réponse 200**

```json
{
  "status": "success",
  "my_permissions": {
    "role": { "id": "role-...", "name": "Administrateur", "system_key": "admin" },
    "permissions": ["pos.access", "catalog.manage", "..."],
    "is_admin": true
  }
}
```

- `role` est `null` si l'appelant n'a pas encore de `role_id` (monde
  historique pré-lot-4).
- `permissions` est la liste effective des clés du catalogue accordées à
  l'appelant, calculée via `Has()` — toujours cohérente avec ce que
  `RequirePermission` déciderait réellement pour chaque droit (que l'appelant
  soit sur l'ancien monde booléen ou sur un rôle).
- `is_admin` vaut `true` si l'appelant est administrateur, que ce soit par la
  colonne booléenne historique ou par `system_key = 'admin'`.

---

## GET /roles

`staff.manage`.

**Réponse 200**

```json
{
  "status": "success",
  "roles": [
    {
      "id": "role-...", "merchant_id": "12", "name": "Administrateur",
      "description": "", "system_key": "admin", "version": 1,
      "created_at": "...", "updated_at": "...", "archived_at": null,
      "permission_count": 15,
      "member_count": 2
    },
    ...
  ]
}
```

Rôles non archivés uniquement, triés par nom. `member_count` compte tous les
porteurs (`users_rights.role_id = cette ligne`), activés ou non.

---

## POST /roles

`staff.manage`.

**Requête**

```json
{
  "name": "Chef de cuisine",
  "description": "",
  "duplicate_from_role_id": "role-source-optionnel"
}
```

`duplicate_from_role_id` est optionnel. S'il est fourni, les droits du rôle
source (dans l'établissement de l'appelant — 404 sinon) sont copiés sur le
nouveau rôle ; `name`/`description` viennent toujours de la requête, jamais
de la source. Sans `duplicate_from_role_id`, le rôle est créé sans aucun
droit (à poser ensuite via `PUT /roles/{id}/permissions`).

**Réponse 201**

```json
{ "status": "success", "role": RoleWithPermissions }
```

**Erreurs** : `role_name_required` (400) ; `role_not_found` (404, si
`duplicate_from_role_id` est invalide/d'un autre établissement).

---

## GET /roles/{id}

`staff.manage`.

**Réponse 200** : `{ "status": "success", "role": RoleWithPermissions }`
(inclut `version`, nécessaire pour les écritures suivantes).

**Erreurs** : `role_not_found` (404).

---

## PATCH /roles/{id}

`staff.manage`. Nom et/ou description — pas les droits.

**Requête**

```json
{ "name": "Nouveau nom", "description": "Nouvelle description", "version": 3 }
```

`name`/`description` sont chacun optionnels individuellement (omettre =
inchangé) ; `version` est obligatoire.

**Réponse 200** : `{ "status": "success", "role": RoleWithPermissions }`
(`version` incrémentée de 1).

**Erreurs** : `role_version_required` (400) ; `role_name_required` (400, si
`name` fourni mais vide) ; `role_immutable` (409, **G4** — rôle admin) ;
`version_conflict` (409, voir plus haut) ; `role_not_found` (404).

---

## PUT /roles/{id}/permissions

`staff.manage`. Remplace l'ensemble des droits (pas un diff).

**Requête**

```json
{ "permission_keys": ["pos.access", "catalog.manage"], "version": 3 }
```

**Réponse 200** : `{ "status": "success", "role": RoleWithPermissions }`
(`permissions` = exactement l'ensemble soumis, dédoublonné ; `version`
incrémentée de 1).

**Erreurs** : `role_version_required` (400) ; `role_permission_key_unknown`
(400) ; `role_immutable` (409, **G4**) ; `role_self_modification` (409,
**G1** — ce rôle est actuellement le vôtre) ; `role_staff_manage_required`
(409, **G2**) ; `version_conflict` (409) ; `role_not_found` (404).

**Effet de bord** : invalide immédiatement la session en cache de tous les
porteurs actifs du rôle (§3) — leur prochain appel relit les droits à jour au
lieu d'attendre jusqu'à 60 minutes.

---

## GET /roles/{id}/members

`staff.manage`.

**Réponse 200**

```json
{
  "status": "success",
  "members": [
    { "user_id": "user-...", "first_name": "Jean", "last_name": "Dupont", "email": "jean@example.com", "enabled": true }
  ]
}
```

Tous les porteurs, activés ou non (un lien désactivé reste un porteur réel —
voir `member_count` de `GET /roles`).

**Erreurs** : `role_not_found` (404).

---

## POST /roles/{id}/archive

`staff.manage`. Corps vide.

**Réponse 200** : `{ "status": "success", "role": Role }` (`archived_at`
posé). Idempotent : ré-archiver un rôle déjà archivé renvoie 200 sans rien
changer.

**Erreurs** : `role_immutable` (409, **G4**) ; `role_has_members` (409,
**G5**, forme enrichie avec `holder_count` — voir plus haut) ;
`role_is_merchant_default` (409, **G6** — rôle `staff` encore
`default_role_id`) ; `role_not_found` (404).

---

## PUT /users/{id}/role

`staff.manage`. `{id}` = `user_id` (pas l'id numérique de la ligne
`users_rights`). Route dans le groupe `/users` existant.

**Requête** : `{ "role_id": "role-..." }`

**Réponse 200**

```json
{ "status": "success", "user_id": "user-...", "role": Role }
```

**Erreurs** : `role_self_modification` (409, **G1** — `{id}` est
l'appelant) ; `role_not_found` (404, rôle inconnu/archivé/autre
établissement) ; `merchant_user_not_found` (404, aucun lien actif
`(merchant, user)` — couvre aussi le lien désactivé) ;
`role_staff_manage_required` (409, **G2**).

Fonctionne aussi bien pour un utilisateur dont `role_id` est actuellement
`NULL` (cas réel signalé par le runbook du lot 4) que pour un changement de
rôle vers rôle.

**Effet de bord** : invalide la session en cache de l'utilisateur cible (§3).

---

## PUT /merchant/default-role

`staff.manage`. Route dans un nouveau groupe `/merchant`.

**Requête** : `{ "role_id": "role-..." }`

**Réponse 200** : `{ "status": "success", "role": Role }`

**Erreurs** : `role_not_found` (404, rôle inconnu/archivé/autre
établissement) ; `invalid_input` (400, `role_id` vide).

Aucun garde-fou ne s'applique : ce champ ne gouverne que le `role_id` des
**futurs** comptes créés (voir `internal/modules/pos/create_service.go`,
`internal/modules/users/create_repository.go`,
`internal/modules/users/admin_repository.go`) — il ne change les droits
d'aucun utilisateur existant, donc aucune invalidation de cache n'est
nécessaire ici.

---

## Garde-fous — résumé

| # | Règle | Où elle s'applique |
|---|---|---|
| G1 | Impossible de changer son propre rôle, ni les droits d'un rôle qu'on porte soi-même | `PUT /users/{id}/role` (id = soi), `PUT /roles/{id}/permissions` (id = son rôle actuel) |
| G2 | Impossible de laisser l'établissement sans aucun utilisateur actif détenant `staff.manage` | `PUT /roles/{id}/permissions`, `PUT /users/{id}/role` |
| G4 | Le rôle `admin` n'est ni renommable, ni archivable, ni modifiable dans ses droits | `PATCH /roles/{id}`, `PUT /roles/{id}/permissions`, `POST /roles/{id}/archive` |
| G5 | Un rôle porté par au moins un utilisateur (activé ou non) ne peut pas être archivé | `POST /roles/{id}/archive` |
| G6 | Le rôle `staff` n'est pas archivable tant qu'il est `merchant.default_role_id` | `POST /roles/{id}/archive` |
| — | Verrouillage optimiste : `version` obligatoire, incrémentée à chaque écriture | `PATCH /roles/{id}`, `PUT /roles/{id}/permissions` |

## Journal d'audit

Une ligne par écriture (`AuditService.LogChange`), jamais une par porteur
affecté :

| action | resource_type | old / new |
|---|---|---|
| `role.created` | `role` | new: `name`, `permissions[]` |
| `role.renamed` | `role` | old/new: `name`, `description` |
| `role.permissions.changed` | `role` | old/new: `permissions[]` |
| `role.archived` | `role` | old: `name`, `permissions[]` |
| `user.role.changed` | `merchant_user` | old/new: `role_id`, `role_name` |
| `merchant.default_role.changed` | `merchant` | old/new: `role_id`, `role_name` |

La lecture du journal viendra dans un lot ultérieur — hors périmètre ici.

## Ce qui n'a PAS pu être vérifié dans cette session

Aucun Postgres ni Redis local disponible dans cet environnement : les tests
`internal/modules/roles/postgres_integration_test.go` (tag
`postgres_integration`) ont été écrits et relus avec soin, y compris
`TestRolesService_Postgres_CacheInvalidation` (§3, nécessite `REDIS_URL` en
plus de `POSTGRES_URL` — skip proprement si absent), mais n'ont pas pu être
réellement exécutés ici. À lancer avant mise en recette :

```
DB_DIALECT=postgres POSTGRES_URL=... REDIS_URL=... go test -tags postgres_integration ./internal/modules/roles/...
```
