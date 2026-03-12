# 🔐 Guide du Middleware de Permissions

## 📋 Vue d'ensemble

Le middleware `RequirePermission` s'appuie sur le middleware d'authentification pour vérifier les droits d'accès des utilisateurs. Il fonctionne avec un système de **Factory Functions** permettant une grande flexibilité.

## 🏗️ Architecture

### Stack de middlewares
```
1. Auth()                  → Authentifie et injecte user dans le contexte
2. RequirePermission(...)  → Vérifie les permissions de l'utilisateur
3. Handler                 → Traite la requête métier
```

### Type PermissionFunc
```go
type PermissionFunc func(user *auth.UserLoginRow) bool
```

Une `PermissionFunc` est une simple fonction qui prend un utilisateur et retourne `true` si la permission est accordée.

## 🎯 Méthodes ajoutées à UserLoginRow

Toutes les permissions sont centralisées dans des méthodes de `UserLoginRow` :

| Méthode | Champ vérifié | Description |
|---------|---------------|-------------|
| `IsAdmin()` | `Admin` | Administrateur (accès total) |
| `HasAccessReception()` | `AccessReception` | Accès réception/caisse |
| `HasAccessDelivery()` | `AccessDelivery` | Accès livraison |
| `HasAccessWaiter()` | `AccessWaiter` | Accès serveur |
| `CanPrintCashReport()` | `PrintMerchantCashReport` | Impression rapports caisse |
| `CanOpenCashDrawer()` | `OpenCashDrawer` | Ouverture tiroir-caisse |
| `HasMenuAccess()` | `CanManageMenu` | Gestion du menu |
| `HasPlanningAccess()` | `CanManagePlannings` | Gestion des plannings |
| `HasUserManagementAccess()` | `CanManageUsers` | Gestion des utilisateurs |
| `HasSettingsAccess()` | `CanManageSettings` | Gestion des paramètres |
| `HasHACCPAccess()` | `CanManageHACCP` | Gestion HACCP |
| `HasReportsViewAccess()` | `CanViewReports` | Consultation des rapports |
| `HasReportsExportAccess()` | `CanExportReports` | Export des rapports |
| `HasFinancialsViewAccess()` | `CanViewFinancials` | Consultation données financières |
| `HasFinancialsExportAccess()` | `CanExportFinancials` | Export données financières |
| `HasCustomerManagementAccess()` | `CanManageCustomers` | Gestion des clients |
| `HasCustomerExportAccess()` | `CanExportCustomers` | Export des clients |

**Note** : Toutes les méthodes retournent `true` pour les administrateurs (`Admin = true`)

## 🚀 Utilisation

### 1. Permission simple sur un groupe de routes

```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)                                    // 1. Authentification
    r.Use(middleware.RequirePermission(middleware.HasMenuAccess)) // 2. Permission
    
    r.Get("/", menuH.GetMenu)
    r.Post("/product/create", menuH.CreateProduct)
    r.Patch("/product/{id}", menuH.UpdateProduct)
    r.Delete("/product/{id}", menuH.DeleteProduct)
})
```

### 2. Permission sur une route spécifique

```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    r.Get("/", menuH.GetMenu) // Accessible à tous les authentifiés
    
    // Seuls ceux qui ont l'accès menu peuvent créer
    r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
      Post("/product/create", menuH.CreateProduct)
})
```

### 3. Permissions multiples (logique AND)

Toutes les permissions doivent être vraies :

```go
r.Route("/admin/settings", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(
        middleware.IsAdmin,              // Doit être admin
        middleware.HasSettingsAccess,    // ET avoir accès paramètres
    ))
    
    r.Get("/", settingsH.GetSettings)
    r.Put("/", settingsH.UpdateSettings)
})
```

### 4. Permissions alternatives (logique OR)

Au moins une permission doit être vraie :

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
    
    r.Get("/reports", cashH.GetReports)
})
```

### 5. Permission personnalisée inline

```go
r.Route("/special", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(func(user *auth.UserLoginRow) bool {
        // Logique personnalisée
        return user.MerchantID == "specific-merchant" && user.HasMenuAccess()
    }))
    
    r.Get("/data", specialH.GetData)
})
```

## 📝 Exemple complet : Routes Menu

### Avant (sans permissions)
```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    r.Get("/", menuH.GetMenu)
    r.Post("/product/create", menuH.CreateProduct)
    r.Patch("/product/{id}", menuH.UpdateProduct)
    r.Delete("/product/{id}", menuH.DeleteProduct)
})
```

**Problème** : Tous les utilisateurs authentifiés peuvent modifier le menu !

### Après (avec permissions)
```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Lecture : accessible à tous les authentifiés
    r.Get("/", menuH.GetMenu)
    r.Get("/attributes", menuH.GetAttributes)
    
    // Modification : réservée aux gestionnaires de menu
    r.Route("/product", func(r chi.Router) {
        r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
        
        r.Post("/create", menuH.CreateProduct)
        r.Patch("/{id}", menuH.UpdateProduct)
        r.Delete("/{id}", menuH.DeleteProduct)
    })
})
```

### Ou, plus simplement (tout le menu protégé)
```go
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
    
    r.Get("/", menuH.GetMenu)
    r.Post("/product/create", menuH.CreateProduct)
    r.Patch("/product/{id}", menuH.UpdateProduct)
    r.Delete("/product/{id}", menuH.DeleteProduct)
})
```

## 🎓 Cas d'usage avancés

### Cas 1 : Permissions différentes par méthode HTTP

```go
r.Route("/reports", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Consultation : nécessite CanViewReports
    r.With(middleware.RequirePermission(middleware.HasReportsViewAccess)).
      Get("/", reportsH.ListReports)
    
    // Export : nécessite CanExportReports (permission plus élevée)
    r.With(middleware.RequirePermission(middleware.HasReportsExportAccess)).
      Get("/export", reportsH.ExportReports)
})
```

### Cas 2 : Hiérarchie de permissions

```go
// Admin peut tout faire, ou droit spécifique pour les autres
r.Route("/users", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(
        middleware.AnyOf(
            middleware.IsAdmin,
            middleware.HasUserManagementAccess,
        ),
    ))
    
    r.Get("/", usersH.ListUsers)
    r.Post("/", usersH.CreateUser)
    
    // Suppression : admin uniquement
    r.With(middleware.RequirePermission(middleware.IsAdmin)).
      Delete("/{id}", usersH.DeleteUser)
})
```

### Cas 3 : Permissions combinées

```go
// Nécessite à la fois l'accès financier ET l'export
r.Route("/financials", func(r chi.Router) {
    r.Use(authMiddleware)
    
    // Voir les finances
    r.With(middleware.RequirePermission(middleware.HasFinancialsViewAccess)).
      Get("/", financialsH.GetFinancials)
    
    // Exporter nécessite DEUX permissions
    r.With(middleware.RequirePermission(
        middleware.AllOf(
            middleware.HasFinancialsViewAccess,
            middleware.HasFinancialsExportAccess,
        ),
    )).Get("/export", financialsH.ExportFinancials)
})
```

### Cas 4 : Permission basée sur le contexte métier

```go
// Vérifier que l'utilisateur appartient au bon merchant
func BelongsToMerchant(merchantID string) middleware.PermissionFunc {
    return func(user *auth.UserLoginRow) bool {
        return user.MerchantID == merchantID
    }
}

// Usage
r.Route("/merchant/{merchantID}/data", func(r chi.Router) {
    r.Use(authMiddleware)
    
    r.Get("/", func(w http.ResponseWriter, r *http.Request) {
        merchantID := chi.URLParam(r, "merchantID")
        
        // Vérifier dynamiquement
        user := middleware.GetUser(r)
        if !BelongsToMerchant(merchantID)(user) {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        
        // Traiter la requête...
    })
})
```

## 🧪 Testing

### Test du middleware RequirePermission

```go
func TestRequirePermission(t *testing.T) {
    // Mock user avec permissions
    userWithAccess := &auth.UserLoginRow{
        UserID:       "1",
        CanManageMenu: true,
    }
    
    userWithoutAccess := &auth.UserLoginRow{
        UserID:       "2",
        CanManageMenu: false,
    }
    
    // Handler de test
    handler := middleware.RequirePermission(middleware.HasMenuAccess)(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusOK)
        }),
    )
    
    // Test 1 : Avec permission
    req1 := httptest.NewRequest("GET", "/menu", nil)
    ctx1 := context.WithValue(req1.Context(), userContextKey, userWithAccess)
    req1 = req1.WithContext(ctx1)
    w1 := httptest.NewRecorder()
    
    handler.ServeHTTP(w1, req1)
    assert.Equal(t, http.StatusOK, w1.Code)
    
    // Test 2 : Sans permission
    req2 := httptest.NewRequest("GET", "/menu", nil)
    ctx2 := context.WithValue(req2.Context(), userContextKey, userWithoutAccess)
    req2 = req2.WithContext(ctx2)
    w2 := httptest.NewRecorder()
    
    handler.ServeHTTP(w2, req2)
    assert.Equal(t, http.StatusForbidden, w2.Code)
}
```

## 📊 Tableau récapitulatif des permissions par module

| Module | Route | Permission requise | Helper |
|--------|-------|-------------------|--------|
| Menu | `/menu/*` | Gestion menu | `HasMenuAccess` |
| POS | `/pos/*` | Réception | `HasAccessReception` |
| Orders | `/orders/*` | Reception/Delivery/Waiter | `AnyOf(...)` |
| Users | `/users/*` | Gestion utilisateurs | `HasUserManagementAccess` |
| Settings | `/settings/*` | Gestion paramètres | `HasSettingsAccess` |
| Reports | `/reports/*` (view) | Consultation rapports | `HasReportsViewAccess` |
| Reports | `/reports/export` | Export rapports | `HasReportsExportAccess` |
| Financials | `/financials/*` | Consultation finances | `HasFinancialsViewAccess` |
| Cash | `/cash/*` | Réception ou caisse | `AnyOf(HasAccessReception, CanPrintCashReport)` |
| Customers | `/customers/*` | Gestion clients | `HasCustomerManagementAccess` |
| Bookings | `/bookings/*` | Réception ou waiter | `AnyOf(HasAccessReception, HasAccessWaiter)` |
| Stocks | `/stocks/*` | Gestion menu (lié au menu) | `HasMenuAccess` |
| Admin | `/admin/*` | Admin uniquement | `IsAdmin` |

## ⚠️ Points d'attention

### 1. Ordre des middlewares
```go
// ✅ BON : Auth PUIS Permission
r.Use(authMiddleware)
r.Use(middleware.RequirePermission(middleware.HasMenuAccess))

// ❌ MAUVAIS : Permission avant Auth
r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
r.Use(authMiddleware)
```

### 2. Permissions sur routes publiques
```go
// ❌ Ne PAS mettre de permission sur les routes publiques
r.Post("/auth/login", authH.Login) // Pas de middleware du tout

// ❌ Ne PAS mettre de permission sur les webhooks
r.Post("/webhooks/stripe", stripeH.HandleWebhook) // Pas de middleware auth
```

### 3. Admin bypass
Toutes les méthodes de permissions retournent `true` pour les administrateurs. Si tu veux une permission stricte sans admin bypass :

```go
func StrictMenuAccess(user *auth.UserLoginRow) bool {
    return user.CanManageMenu // N'inclut PAS user.Admin
}
```

### 4. Messages d'erreur
Le middleware retourne un JSON simple :
```json
{
    "error": "accès refusé"
}
```

Pour des messages personnalisés, utilise une fonction inline :
```go
r.Use(middleware.RequirePermission(func(user *auth.UserLoginRow) bool {
    if !user.HasMenuAccess() {
        // Logger un message personnalisé
        log.Warn("User %s tried to access menu without permission", user.UserID)
        return false
    }
    return true
}))
```

## ✅ Checklist d'implémentation

- [x] Méthodes de permissions ajoutées à `UserLoginRow`
- [x] Helpers de permissions créés dans `middleware/permissions.go`
- [x] Middleware `RequirePermission` existant utilisé
- [ ] Appliquer les permissions sur les routes sensibles
- [ ] Tester les accès autorisés
- [ ] Tester les refus d'accès (403)
- [ ] Documenter les permissions par module
- [ ] Former l'équipe sur le nouveau système

## 🎯 Prochaines étapes

1. **Identifier les routes sensibles** : Lister toutes les routes qui nécessitent des permissions
2. **Appliquer les permissions** : Ajouter `RequirePermission` sur ces routes
3. **Tester** : Vérifier que les accès sont bien restreints
4. **Documenter** : Mettre à jour la doc API avec les permissions requises

---

**💡 Pro Tip** : Commence par protéger les routes les plus critiques (création, modification, suppression) avant les routes de lecture.
