# 🔐 Migration vers le Middleware d'Authentification

## 📋 Vue d'ensemble

Le middleware d'authentification centralise la validation des tokens et l'injection de l'utilisateur dans le contexte de la requête. Plus besoin d'appeler manuellement `authService.GetUserByToken()` dans chaque handler !

## 🏗️ Architecture

### Fonctionnement
```
1. Client envoie une requête avec header Authorization: Bearer <token>
2. Middleware Auth intercepte la requête
3. Valide le token via authRepo.GetUserByToken()
4. Injecte l'utilisateur dans r.Context()
5. Passe la main au handler suivant
6. Handler récupère l'utilisateur via middleware.GetUser(r)
```

### Avantages
- ✅ **DRY** : Plus de duplication du code d'authentification
- ✅ **Sécurité** : Clé de contexte typée (évite les collisions)
- ✅ **Performance** : Cache Redis géré au niveau repo
- ✅ **Maintenabilité** : Logique d'auth centralisée
- ✅ **Testabilité** : Interface AuthRepo mockable

## 🔧 Utilisation dans routes.go

### 1. Initialiser le middleware

```go
// Dans SetupRoutes()
authRepo := authModule.NewAuthRepository(mysqlDB)
authMiddleware := middleware.Auth(authRepo)
```

### 2. Appliquer sur des routes protégées

**Option A : Protéger un groupe de routes**
```go
// Toutes les routes /menu/* sont protégées
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    
    r.Get("/", menuH.GetMenu)
    r.Get("/units", menuH.GetUnitsOfMeasures)
    r.Get("/attributes", menuH.GetAttributes)
    r.Put("/product/{id}/allergens", menuH.UpdateProductAllergens)
})
```

**Option B : Protéger une route spécifique**
```go
r.With(authMiddleware).Get("/menu", menuH.GetMenu)
r.With(authMiddleware).Post("/orders", ordersH.CreateOrder)
```

**Option C : Mix de routes publiques et protégées**
```go
r.Route("/auth", func(r chi.Router) {
    r.Post("/login", authH.Login)          // Public
    r.With(authMiddleware).Get("/me", authH.GetCurrentUser)  // Protégé
})
```

## 📝 Exemple complet : Avant / Après

### ❌ AVANT (méthode manuelle)

```go
// Handler avec extraction et validation manuelle du token
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
    // 1. Extraire le token manuellement
    token := helpers.ExtractToken(r)
    if strings.TrimSpace(token) == "" {
        models.SendJSON(w, http.StatusUnauthorized, "menu", "get", 
            map[string]string{"error": "missing_token"})
        return
    }

    ctx := r.Context()
    log := logger.FromContext(ctx)

    // Parse params...
    lastMenuParam := r.URL.Query().Get("last_menu_update")
    var lastMenu *time.Time
    if lastMenuParam != "" {
        if unix, err := strconv.ParseInt(lastMenuParam, 10, 64); err == nil {
            t := time.Unix(unix, 0).UTC()
            lastMenu = &t
        }
    }

    // 2. Appeler le service qui va valider le token
    menu, err := h.service.GetMenu(ctx, token, lastMenu)
    if err != nil {
        log.Error("[ERROR] GetMenu error " + err.Error())
        models.SendErrorJSON(w, "menu", "get", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}
```

```go
// Service avec validation du token
func (s *MenuService) GetMenu(ctx context.Context, token string, lastUpdate *time.Time) (*MenuResponse, error) {
    // Validation du token dans CHAQUE méthode
    user, err := s.userRepo.GetUserByToken(ctx, token)
    if err != nil {
        return nil, errors.New("invalid_token")
    }
    if user == nil {
        return nil, errors.New("user_not_found")
    }

    // Logique métier...
    return s.repo.GetFullMenu(ctx, user.MerchantID, lastUpdate)
}
```

### ✅ APRÈS (avec middleware)

```go
// Handler simplifié - le token est déjà validé !
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
    // 1. Récupérer l'utilisateur déjà authentifié
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return // L'erreur HTTP est déjà envoyée
    }

    ctx := r.Context()
    log := logger.FromContext(ctx)

    // Parse params...
    lastMenuParam := r.URL.Query().Get("last_menu_update")
    var lastMenu *time.Time
    if lastMenuParam != "" {
        if unix, err := strconv.ParseInt(lastMenuParam, 10, 64); err == nil {
            t := time.Unix(unix, 0).UTC()
            lastMenu = &t
        }
    }

    // 2. Appeler le service directement avec l'utilisateur
    menu, err := h.service.GetMenu(ctx, user, lastMenu)
    if err != nil {
        log.Error("[ERROR] GetMenu error " + err.Error())
        models.SendErrorJSON(w, "menu", "get", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}
```

```go
// Service sans gestion du token
func (s *MenuService) GetMenu(ctx context.Context, user *auth.UserLoginRow, lastUpdate *time.Time) (*MenuResponse, error) {
    // Plus de validation de token - on passe directement à la logique métier !
    return s.repo.GetFullMenu(ctx, user.MerchantID, lastUpdate)
}
```

### 🎯 Réductions
- **Handler** : ~10 lignes → ~3 lignes (validation)
- **Service** : ~8 lignes → 0 ligne (plus de validation)
- **Lisibilité** : ⭐⭐⭐ → ⭐⭐⭐⭐⭐

## 🛠️ Helpers disponibles

### `middleware.GetUser(r *http.Request)`
Retourne l'utilisateur authentifié ou `nil`.
```go
user := middleware.GetUser(r)
if user == nil {
    // Gérer l'absence d'utilisateur
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}
// Utiliser user...
```

### `middleware.MustGetUser(w, r)`
Retourne l'utilisateur et gère automatiquement l'erreur HTTP si absent.
```go
user, ok := middleware.MustGetUser(w, r)
if !ok {
    return // Erreur déjà envoyée au client
}
// Utiliser user en toute sécurité
```

## 📊 Plan de migration

### Étape 1 : Routes (routes.go)
```go
// Dans SetupRoutes()
authRepo := authModule.NewAuthRepository(mysqlDB)
authMiddleware := middleware.Auth(authRepo)

// Appliquer sur les routes protégées
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    // ... toutes les routes menu
})
```

### Étape 2 : Handlers
Remplacer :
```go
token := helpers.ExtractToken(r)
if strings.TrimSpace(token) == "" {
    models.SendJSON(w, http.StatusUnauthorized, ...)
    return
}
```

Par :
```go
user, ok := middleware.MustGetUser(w, r)
if !ok {
    return
}
```

### Étape 3 : Services
Changer la signature des méthodes :
```go
// Avant
func (s *MenuService) GetMenu(ctx context.Context, token string, ...) (*MenuResponse, error)

// Après
func (s *MenuService) GetMenu(ctx context.Context, user *auth.UserLoginRow, ...) (*MenuResponse, error)
```

Supprimer la validation du token dans chaque méthode :
```go
// ❌ Supprimer ces lignes
user, err := s.userRepo.GetUserByToken(ctx, token)
if err != nil || user == nil {
    return nil, errors.New("invalid_token")
}
```

### Étape 4 : Tests
Le middleware se teste en injectant un faux `AuthRepo` :
```go
type mockAuthRepo struct{}

func (m *mockAuthRepo) GetUserByToken(ctx context.Context, token string) (*auth.UserLoginRow, error) {
    if token == "valid-token" {
        return &auth.UserLoginRow{UserID: 1, Name: "Test User"}, nil
    }
    return nil, errors.New("invalid token")
}

// Dans le test
authMiddleware := middleware.Auth(&mockAuthRepo{})
```

## 🔍 Modules à migrer

Modules utilisant actuellement `GetUserByToken` :
- ✅ `internal/middleware/auth.go` — Middleware implémenté
- 🔄 `internal/modules/menu/` — ~13 méthodes
- 🔄 `internal/modules/user_services/`
- 🔄 `internal/modules/auth/` (service)
- 🔄 `internal/modules/orders/`
- 🔄 `internal/modules/pos/`
- 🔄 `internal/modules/customers/`
- 🔄 `internal/modules/cash_registers/`
- 🔄 `internal/modules/bookings/`
- 🔄 `internal/modules/delivery_sessions/`
- 🔄 `internal/modules/locations/`
- 🔄 `internal/modules/stocks/`
- 🔄 Tous les autres modules utilisant l'auth

## ⚠️ Points d'attention

### Routes publiques
Certaines routes ne doivent **PAS** avoir le middleware :
- `POST /auth/login` — Authentification initiale
- `POST /webhooks/*` — Webhooks externes (Stripe, Deliveroo, Uber)
- `GET /health` — Health checks

### Compatibilité Token Query Param
L'ancien code supporte `?token=...` en query param. Le middleware actuel ne le supporte pas. Si nécessaire, ajouter :
```go
func Auth(repo AuthRepo) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            
            // Support legacy ?token=...
            if authHeader == "" {
                if queryToken := r.URL.Query().Get("token"); queryToken != "" {
                    authHeader = "Bearer " + queryToken
                }
            }
            
            if authHeader == "" {
                http.Error(w, `{"error":"token manquant"}`, http.StatusUnauthorized)
                return
            }
            // ... reste du code
        })
    }
}
```

### Gestion des permissions
Le middleware ne vérifie que l'authentification. Pour les permissions, utiliser le middleware `RequirePermission` existant **après** le middleware Auth :
```go
r.Route("/pos", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission("access_wrreception"))
    
    r.Get("/products", posH.GetProducts)
})
```

## ✅ Checklist de migration

Pour chaque module :

- [ ] Ajouter `authMiddleware` sur le groupe de routes dans `routes.go`
- [ ] Mettre à jour les handlers :
  - [ ] Remplacer `helpers.ExtractToken()` par `middleware.MustGetUser()`
  - [ ] Supprimer la validation manuelle du token
- [ ] Mettre à jour les services :
  - [ ] Changer `token string` → `user *auth.UserLoginRow` dans les signatures
  - [ ] Supprimer les appels à `GetUserByToken()` dans les méthodes
- [ ] Tester les endpoints
- [ ] Vérifier les logs (pas d'erreurs d'auth)

## 🚀 Résultat final

### Performance
- ✅ Cache Redis utilisé une seule fois par requête (au niveau middleware)
- ✅ Pas de validation multiple du même token

### Maintenabilité
- ✅ Logique d'auth centralisée dans le middleware
- ✅ Handlers plus lisibles et focalisés sur la logique métier
- ✅ Services découplés de l'authentification

### Sécurité
- ✅ Type-safe context key (pas de collision)
- ✅ Gestion d'erreur cohérente
- ✅ Token validation uniforme

---

**Note** : Cette migration peut se faire **progressivement** par module. Les anciennes routes continuent de fonctionner pendant la migration des nouvelles.
