# 🎯 Exemple d'application des permissions dans routes.go

## 📍 Route concernée
`POST /menu/product/create` - Création d'un produit dans le menu

## 🔧 Implémentation dans cmd/api/routes.go

### Option 1 : Permission sur tout le groupe `/menu`

**Recommandé si toutes les opérations menu nécessitent la même permission**

```go
// --- MENU ---
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)                                         // 1. Authentification
    r.Use(middleware.RequirePermission(middleware.HasMenuAccess)) // 2. Permission menu
    
    r.Get("/", menuH.GetMenu)
    r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
    r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
    r.Patch("/product/{product_id}", menuH.UpdateProduct)
    r.Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
    r.Get("/attributes", menuH.GetAttributes)
    r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)

    r.Post("/product/create", menuH.CreateProduct) // ✅ Protégé par le groupe

    r.Get("/product/{product_id}", menuH.GetProduct)

    // --- Bulk assign (additive) ---
    r.Route("/bulk", func(r chi.Router) {
        r.Post("/tags/assign", menuH.BulkAssignTag)
        r.Post("/allergens/assign", menuH.BulkAssignAllergen)
    })

    // --- Allergens & Tags (full sync) ---
    r.Put("/product/{product_id}/allergens", menuH.SyncProductAllergens)
    r.Put("/product/{product_id}/tags", menuH.SyncProductTags)

    // --- Plateformes externes ---
    r.Get("/deliveroo", menuH.GetDeliverooMenu)
    r.Patch("/deliveroo/sync", menuH.SyncDeliverooMenu)
    r.Get("/uber-eats", menuH.GetUberEatsMenu)
    r.Patch("/uber-eats/sync", menuH.SyncUberEatsMenu)
})
```

### Option 2 : Permission uniquement sur `/product/create`

**Si tu veux des permissions granulaires par endpoint**

```go
// --- MENU ---
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware) // Tout le monde authentifié peut lire
    
    // Routes de lecture (accessible à tous les authentifiés)
    r.Get("/", menuH.GetMenu)
    r.Get("/attributes", menuH.GetAttributes)
    r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)
    r.Get("/product/{product_id}", menuH.GetProduct)
    r.Get("/deliveroo", menuH.GetDeliverooMenu)
    r.Get("/uber-eats", menuH.GetUberEatsMenu)
    
    // Routes de modification (nécessite HasMenuAccess)
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Post("/product/create", menuH.CreateProduct) // ✅ Protégé individuellement
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/product/{product_id}", menuH.UpdateProduct)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Put("/product/{product_id}/allergens", menuH.SyncProductAllergens)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Put("/product/{product_id}/tags", menuH.SyncProductTags)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/deliveroo/sync", menuH.SyncDeliverooMenu)
    
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Patch("/uber-eats/sync", menuH.SyncUberEatsMenu)
    
    // --- Bulk assign ---
    r.Route("/bulk", func(r chi.Router) {
        r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
        
        r.Post("/tags/assign", menuH.BulkAssignTag)
        r.Post("/allergens/assign", menuH.BulkAssignAllergen)
    })
})
```

### Option 3 : Permissions séparées lecture/écriture

**Si tu veux distinguer consultation vs modification**

```go
// --- MENU ---
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Routes de lecture : accessible à tous les authentifiés
    r.Get("/", menuH.GetMenu)
    r.Get("/attributes", menuH.GetAttributes)
    r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)
    r.Get("/product/{product_id}", menuH.GetProduct)
    r.Get("/deliveroo", menuH.GetDeliverooMenu)
    r.Get("/uber-eats", menuH.GetUberEatsMenu)
    
    // Groupe modification : nécessite HasMenuAccess
    r.Route("/manage", func(r chi.Router) {
        r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
        
        r.Post("/product/create", menuH.CreateProduct) // ✅ Protégé dans le groupe manage
        r.Patch("/product/{product_id}", menuH.UpdateProduct)
        r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
        r.Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
        r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
        r.Put("/product/{product_id}/allergens", menuH.SyncProductAllergens)
        r.Put("/product/{product_id}/tags", menuH.SyncProductTags)
        r.Patch("/deliveroo/sync", menuH.SyncDeliverooMenu)
        r.Patch("/uber-eats/sync", menuH.SyncUberEatsMenu)
        
        r.Route("/bulk", func(r chi.Router) {
            r.Post("/tags/assign", menuH.BulkAssignTag)
            r.Post("/allergens/assign", menuH.BulkAssignAllergen)
        })
    })
})
```

## 🎯 Recommandation : Option 1 (simple et efficace)

**Modifie le fichier `cmd/api/routes.go` ligne ~394** :

```diff
  // --- MENU ---
  r.Route("/menu", func(r chi.Router) {
      r.Use(authMiddleware)
+     r.Use(middleware.RequirePermission(middleware.HasMenuAccess))

      r.Get("/", menuH.GetMenu)
      r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
      // ... reste du code
  })
```

## 🧪 Test de la protection

### 1. Test avec utilisateur autorisé

```bash
# Utilisateur avec CanManageMenu = true
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <token-with-menu-access>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Nouveau Produit",
    "price": 1500,
    "category_id": "cat-123"
  }'

# Réponse attendue : 200 OK avec le produit créé
```

### 2. Test avec utilisateur non autorisé

```bash
# Utilisateur avec CanManageMenu = false
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <token-without-menu-access>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Nouveau Produit",
    "price": 1500,
    "category_id": "cat-123"
  }'

# Réponse attendue : 403 Forbidden
{
  "error": "accès refusé"
}
```

### 3. Test avec administrateur

```bash
# Administrateur (Admin = true)
curl -X POST http://localhost:8080/menu/product/create \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Nouveau Produit",
    "price": 1500,
    "category_id": "cat-123"
  }'

# Réponse attendue : 200 OK
# Les admins bypas sent automatiquement toutes les permissions
```

## 🔍 Vérification dans les logs

Quand un utilisateur sans permission essaie d'accéder :

```
INFO  [middleware] User user-123 attempted to access /menu/product/create without HasMenuAccess
INFO  [middleware] Returned 403 Forbidden
```

## 📋 Checklist d'implémentation

- [ ] Modifier `cmd/api/routes.go` pour ajouter le middleware de permission
- [ ] Compiler le projet : `go build ./...`
- [ ] Tester avec un utilisateur autorisé (doit fonctionner)
- [ ] Tester avec un utilisateur non autorisé (doit retourner 403)
- [ ] Tester avec un administrateur (doit fonctionner)
- [ ] Vérifier les logs
- [ ] Documenter dans l'API les permissions requises

## 🚀 Application sur d'autres routes

Une fois le pattern maîtrisé sur `/menu`, applique-le aux autres modules :

### POS
```go
r.Route("/pos", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasAccessReception))
    // ...
})
```

### Users
```go
r.Route("/users", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasUserManagementAccess))
    // ...
})
```

### Settings
```go
r.Route("/settings", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasSettingsAccess))
    // ...
})
```

### Reports (avec distinction view/export)
```go
r.Route("/reports", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Consultation
    r.With(middleware.RequirePermission(middleware.HasReportsViewAccess)).
      Get("/", reportsH.ListReports)
    
    // Export (permission plus élevée)
    r.With(middleware.RequirePermission(middleware.HasReportsExportAccess)).
      Get("/export", reportsH.ExportReports)
})
```

### Cash/Financials (permissions alternatives)
```go
r.Route("/cash", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(
        middleware.AnyOf(
            middleware.IsAdmin,
            middleware.HasAccessReception,
            middleware.CanPrintCashReport,
        ),
    ))
    // ...
})
```

---

**💡 Note importante** : Le middleware `RequirePermission` DOIT être appliqué APRÈS `authMiddleware`. L'ordre est crucial !
