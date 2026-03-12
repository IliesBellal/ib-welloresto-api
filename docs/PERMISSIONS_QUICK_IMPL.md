# ⚡ Protection immédiate de POST /menu/product/create

## 🎯 Modification à effectuer

### Fichier : `cmd/api/routes.go` (ligne ~394)

**AVANT** :
```go
// --- MENU ---
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)

    r.Get("/", menuH.GetMenu)
    r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
    r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
    r.Patch("/product/{product_id}", menuH.UpdateProduct)
    r.Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
    r.Get("/attributes", menuH.GetAttributes)
    r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)

    r.Post("/product/create", menuH.CreateProduct)
    r.Get("/product/{product_id}", menuH.GetProduct)

    // ... reste du code
})
```

**APRÈS** :
```go
// --- MENU ---
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasMenuAccess)) // ✅ AJOUTER CETTE LIGNE

    r.Get("/", menuH.GetMenu)
    r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
    r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
    r.Patch("/product/{product_id}", menuH.UpdateProduct)
    r.Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
    r.Get("/attributes", menuH.GetAttributes)
    r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)

    r.Post("/product/create", menuH.CreateProduct)
    r.Get("/product/{product_id}", menuH.GetProduct)

    // ... reste du code
})
```

## 📝 Explication

Cette seule ligne de code :
```go
r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
```

Va :
1. ✅ Vérifier que l'utilisateur est authentifié (via `authMiddleware`)
2. ✅ Récupérer l'utilisateur du contexte
3. ✅ Appeler `user.HasMenuAccess()` qui retourne `true` si :
   - `user.Admin == true` (administrateur)
   - OU `user.CanManageMenu == true` (gestionnaire de menu)
4. ❌ Retourner **403 Forbidden** si l'utilisateur n'a pas l'accès
5. ✅ Laisser passer la requête si l'utilisateur a l'accès

## 🧪 Test immédiat

### 1. Compiler
```powershell
go build ./cmd/api
```

### 2. Lancer l'API
```powershell
.\api.exe
# ou
go run ./cmd/api
```

### 3. Tester avec Postman/curl

**Cas 1 : Utilisateur avec permission**
```bash
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <token-user-with-CanManageMenu>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Product","price":1000}'
```
**Résultat attendu** : 200 OK ✅

**Cas 2 : Utilisateur sans permission**
```bash
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <token-user-without-CanManageMenu>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Product","price":1000}'
```
**Résultat attendu** : 403 Forbidden ❌
```json
{
  "error": "accès refusé"
}
```

**Cas 3 : Administrateur**
```bash
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <token-admin>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Product","price":1000}'
```
**Résultat attendu** : 200 OK ✅ (admin bypass)

## 🎓 Comprendre la logique

### Dans UserLoginRow (internal/modules/auth/models.go)
```go
func (u *UserLoginRow) HasMenuAccess() bool {
    return u.Admin || u.CanManageMenu
}
```

### Dans le middleware (internal/middleware/permissions.go)
```go
func HasMenuAccess(user *auth.UserLoginRow) bool {
    return user.HasMenuAccess()
}
```

### Dans RequirePermission (internal/middleware/require_permission.go)
```go
func RequirePermission(permissions ...PermissionFunc) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            user := GetUser(r)
            if user == nil {
                http.Error(w, `{"error":"non authentifié"}`, http.StatusUnauthorized)
                return
            }

            for _, hasPermission := range permissions {
                if !hasPermission(user) {
                    http.Error(w, `{"error":"accès refusé"}`, http.StatusForbidden)
                    return
                }
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

## ✅ Résultat

Toutes les routes du groupe `/menu` (dont `/menu/product/create`) sont maintenant protégées. Seuls les utilisateurs avec :
- `Admin = true`
- OU `CanManageMenu = true`

...peuvent y accéder.

## 🔄 Alternatives

### Si tu veux protéger UNIQUEMENT `/product/create`

```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Routes accessibles à tous les authentifiés
    r.Get("/", menuH.GetMenu)
    r.Get("/attributes", menuH.GetAttributes)
    
    // Route protégée spécifiquement
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Post("/product/create", menuH.CreateProduct)
})
```

### Si tu veux séparer lecture/écriture

```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Lecture : tous les authentifiés
    r.Get("/", menuH.GetMenu)
    r.Get("/product/{id}", menuH.GetProduct)
    
    // Écriture : besoin de HasMenuAccess
    r.Group(func(r chi.Router) {
        r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
        
        r.Post("/product/create", menuH.CreateProduct)
        r.Patch("/product/{id}", menuH.UpdateProduct)
        r.Delete("/product/{id}", menuH.DeleteProduct)
    })
})
```

---

**💡 Recommandation** : Commence par l'option simple (protection du groupe entier), puis affine si nécessaire.
