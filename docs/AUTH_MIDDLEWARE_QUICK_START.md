# 🚀 Auth Middleware - Quick Start

## ⚡ TL;DR

Ton middleware d'authentification est **déjà implémenté** et prêt à l'emploi !

## 🎯 En 3 étapes

### 1️⃣ Dans routes.go
```go
// Créer le middleware
authRepo := authModule.NewAuthRepository(mysqlDB)
authMiddleware := middleware.Auth(authRepo)

// L'appliquer sur tes routes
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware) // 🔒 Tout est protégé
    r.Get("/", menuH.GetMenu)
    r.Post("/product", menuH.CreateProduct)
})
```

### 2️⃣ Dans ton handler
```go
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
    // Une seule ligne pour récupérer l'utilisateur
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return // Erreur déjà gérée
    }
    
    // Utilise directement user
    menu, err := h.service.GetMenu(ctx, user, lastUpdate)
    // ...
}
```

### 3️⃣ Dans ton service
```go
// Change la signature
func (s *MenuService) GetMenu(ctx context.Context, user *auth.UserLoginRow, lastUpdate *time.Time) (*MenuResponse, error) {
    // Plus besoin de GetUserByToken() !
    return s.repo.GetFullMenu(ctx, user.MerchantID, lastUpdate)
}
```

## 📊 Avant / Après

| Avant | Après |
|-------|-------|
| **Handler: 28 lignes** | **Handler: 18 lignes** |
| `ExtractToken()` | `MustGetUser()` |
| Validation manuelle | Automatique |
| **Service: 35 lignes** | **Service: 25 lignes** |
| `GetUserByToken()` dans chaque méthode | Rien ! |
| Token validation × 13 | Token validation × 1 |

## 🔧 Helpers disponibles

```go
// Option 1 : Gestion manuelle
user := middleware.GetUser(r)
if user == nil {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}

// Option 2 : Gestion automatique (recommandé)
user, ok := middleware.MustGetUser(w, r)
if !ok {
    return // HTTP 401 déjà envoyé
}
```

## 📁 Fichiers créés

1. **`internal/middleware/auth.go`**
   - ✅ Middleware `Auth(repo)`
   - ✅ Helper `GetUser(r)`
   - ✅ Helper `MustGetUser(w, r)`

2. **`docs/AUTH_MIDDLEWARE_MIGRATION.md`**
   - Guide complet de migration
   - Explication de l'architecture
   - Checklist par module

3. **`docs/MIGRATION_EXAMPLE_MENU.md`**
   - Exemple concret avec le module Menu
   - Code avant/après pour chaque fichier
   - Tests et validation

## ⚠️ Routes publiques (sans middleware)

```go
// Auth et webhooks = pas de middleware
r.Post("/auth/login", authH.Login)
r.Post("/webhooks/stripe", stripeWH.Handle)
r.Post("/webhooks/deliveroo", deliverooWH.Handle)

// Routes protégées = avec middleware
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    // ...
})
```

## 📦 Modules à migrer

Voici les modules qui appellent actuellement `GetUserByToken()` :

| Module | Méthodes à migrer | Priorité |
|--------|-------------------|----------|
| menu | ~13 | 🔥 Haute |
| orders | ~8 | 🔥 Haute |
| pos | ~6 | ⚡ Moyenne |
| customers | ~4 | ⚡ Moyenne |
| cash_registers | ~5 | ⚡ Moyenne |
| bookings | ~4 | 💡 Basse |
| delivery_sessions | ~3 | 💡 Basse |
| locations | ~3 | 💡 Basse |
| stocks | ~5 | 💡 Basse |
| user_services | ~2 | 💡 Basse |

**Total** : ~53 méthodes à simplifier

## ✅ Avantages

| Aspect | Avant | Après |
|--------|-------|-------|
| **Validation token** | Dans chaque handler + service | Une fois dans le middleware |
| **Appels Redis/DB** | × 53 par requête | × 1 par requête |
| **Code dupliqué** | 53 fois | 0 fois |
| **Maintenabilité** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Lisibilité** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Testabilité** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

## 🧪 Test rapide

```bash
# Terminal 1 : Lancer l'API
go run cmd/api/main.go

# Terminal 2 : Tester
curl -H "Authorization: Bearer ton-token" http://localhost:8080/menu
```

## 🎓 Next Steps

1. **Commence par le module Menu** (le plus gros)
2. **Teste bien** chaque endpoint après migration
3. **Reproduis le pattern** sur les autres modules
4. **Documente** les changements dans ton CHANGELOG

## 📖 Documentation complète

- **Guide détaillé** : `docs/AUTH_MIDDLEWARE_MIGRATION.md`
- **Exemple pratique** : `docs/MIGRATION_EXAMPLE_MENU.md`
- **Code source** : `internal/middleware/auth.go`

---

**💡 Pro Tip** : Le middleware est déjà prêt. Tu n'as qu'à l'utiliser dans `routes.go` et adapter tes handlers/services !
