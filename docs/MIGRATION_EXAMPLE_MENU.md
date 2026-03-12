# 🔄 Exemple de Migration : Module Menu

## 📍 Contexte
Le module `menu` utilise actuellement 13+ méthodes qui appellent `GetUserByToken()`. Voici comment le migrer.

## 🛠️ Étape 1 : Routes (cmd/api/routes.go)

### Avant
```go
menuH := menuModule.NewMenuHandler(menuService)

// Routes non protégées
r.Get("/menu", menuH.GetMenu)
r.Get("/menu/units", menuH.GetUnitsOfMeasures)
r.Get("/menu/attributes", menuH.GetAttributes)
r.Put("/menu/product/{id}/allergens", menuH.UpdateProductAllergens)
// ... etc
```

### Après
```go
menuH := menuModule.NewMenuHandler(menuService)

// Initialiser le middleware d'auth
authRepo := authModule.NewAuthRepository(mysqlDB)
authMiddleware := middleware.Auth(authRepo)

// Toutes les routes /menu sont maintenant protégées
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware) // ✅ Une ligne, tout est protégé
    
    r.Get("/", menuH.GetMenu)
    r.Get("/units", menuH.GetUnitsOfMeasures)
    r.Get("/attributes", menuH.GetAttributes)
    r.Put("/product/{id}/allergens", menuH.UpdateProductAllergens)
    r.Put("/product/{id}/tags", menuH.UpdateProductTags)
    r.Post("/category", menuH.CreateCategory)
    r.Put("/category/{id}", menuH.UpdateCategory)
    r.Delete("/category/{id}", menuH.DeleteCategory)
    r.Post("/product", menuH.CreateProduct)
    r.Put("/product/{id}", menuH.UpdateProduct)
    r.Delete("/product/{id}", menuH.DeleteProduct)
    r.Post("/component", menuH.CreateComponent)
    r.Put("/component/{id}", menuH.UpdateComponent)
    r.Delete("/component/{id}", menuH.DeleteComponent)
})
```

## 📝 Étape 2 : Handler (internal/modules/menu/handler.go)

### GetMenu - Avant
```go
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
    token := helpers.ExtractToken(r)
    if strings.TrimSpace(token) == "" {
        models.SendJSON(w, http.StatusUnauthorized, "menu", "get", map[string]string{"error": "missing_token"})
        return
    }

    ctx := r.Context()
    log := logger.FromContext(ctx)

    lastMenuParam := r.URL.Query().Get("last_menu_update")
    var lastMenu *time.Time
    if lastMenuParam != "" {
        if unix, err := strconv.ParseInt(lastMenuParam, 10, 64); err == nil {
            t := time.Unix(unix, 0).UTC()
            lastMenu = &t
        } else {
            log.Warn("Invalid last_menu_update param: " + lastMenuParam)
        }
    }

    menu, err := h.service.GetMenu(ctx, token, lastMenu)
    if err != nil {
        log.Error("[ERROR] GetMenu error " + err.Error())
        models.SendErrorJSON(w, "menu", "get", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}
```

### GetMenu - Après
```go
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
    // ✅ Utilisateur déjà authentifié par le middleware
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return // Erreur HTTP déjà envoyée
    }

    ctx := r.Context()
    log := logger.FromContext(ctx)

    lastMenuParam := r.URL.Query().Get("last_menu_update")
    var lastMenu *time.Time
    if lastMenuParam != "" {
        if unix, err := strconv.ParseInt(lastMenuParam, 10, 64); err == nil {
            t := time.Unix(unix, 0).UTC()
            lastMenu = &t
        } else {
            log.Warn("Invalid last_menu_update param: " + lastMenuParam)
        }
    }

    // ✅ Passer l'utilisateur au lieu du token
    menu, err := h.service.GetMenu(ctx, user, lastMenu)
    if err != nil {
        log.Error("[ERROR] GetMenu error " + err.Error())
        models.SendErrorJSON(w, "menu", "get", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}
```

### Autres méthodes du handler
```go
func (h *MenuHandler) GetUnitsOfMeasures(w http.ResponseWriter, r *http.Request) {
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return
    }

    ctx := r.Context()
    updated, err := h.service.GetUnitsOfMeasures(ctx, user)
    if err != nil {
        models.SendErrorJSON(w, "menu", "get_units_of_measures", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get_units_of_measures", updated)
}

func (h *MenuHandler) GetAttributes(w http.ResponseWriter, r *http.Request) {
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return
    }

    ctx := r.Context()
    attributes, err := h.service.GetAttributes(ctx, user)
    if err != nil {
        models.SendErrorJSON(w, "menu", "get_attributes", err)
        return
    }

    models.SendJSON(w, http.StatusOK, "menu", "get_attributes", attributes)
}

func (h *MenuHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
    user, ok := middleware.MustGetUser(w, r)
    if !ok {
        return
    }

    ctx := r.Context()
    
    var req models.CreateProductRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        models.SendJSON(w, http.StatusBadRequest, "menu", "create_product", map[string]string{"error": "invalid_json"})
        return
    }

    product, err := h.service.CreateProduct(ctx, user, &req)
    if err != nil {
        models.SendErrorJSON(w, "menu", "create_product", err)
        return
    }

    models.SendJSON(w, http.StatusCreated, "menu", "create_product", product)
}
```

## ⚙️ Étape 3 : Service (internal/modules/menu/service.go)

### Avant
```go
type MenuService struct {
    repo            *MenuRepository
    userRepo        auth.AuthRepository  // ❌ Plus nécessaire !
    deliverooClient deliveroo.DeliverooService
    uberClient      ubereats.UberService
}

func NewMenuService(repo *MenuRepository, userRepo auth.AuthRepository, deliveroo deliveroo.DeliverooService, uber ubereats.UberService) *MenuService {
    return &MenuService{
        repo:            repo,
        userRepo:        userRepo,  // ❌ À supprimer
        deliverooClient: deliveroo,
        uberClient:      uber,
    }
}

func (s *MenuService) GetMenu(ctx context.Context, token string, lastUpdate *time.Time) (*MenuResponse, error) {
    // ❌ Validation du token dans CHAQUE méthode
    user, err := s.userRepo.GetUserByToken(ctx, token)
    if err != nil {
        return nil, errors.New("invalid_token")
    }
    if user == nil {
        return nil, errors.New("user_not_found")
    }

    // Logique métier
    return s.repo.GetFullMenu(ctx, user.MerchantID, lastUpdate)
}

func (s *MenuService) GetUnitsOfMeasures(ctx context.Context, token string) ([]UnitOfMeasure, error) {
    // ❌ Encore la validation du token
    user, err := s.userRepo.GetUserByToken(ctx, token)
    if err != nil {
        return nil, errors.New("invalid_token")
    }
    if user == nil {
        return nil, errors.New("user_not_found")
    }

    return s.repo.GetUnitsOfMeasures(ctx, user.MerchantID)
}

// ... et ainsi de suite pour les 11 autres méthodes
```

### Après
```go
type MenuService struct {
    repo            *MenuRepository
    // ✅ userRepo supprimé - plus besoin !
    deliverooClient deliveroo.DeliverooService
    uberClient      ubereats.UberService
}

func NewMenuService(repo *MenuRepository, deliveroo deliveroo.DeliverooService, uber ubereats.UberService) *MenuService {
    return &MenuService{
        repo:            repo,
        deliverooClient: deliveroo,
        uberClient:      uber,
    }
}

func (s *MenuService) GetMenu(ctx context.Context, user *auth.UserLoginRow, lastUpdate *time.Time) (*MenuResponse, error) {
    // ✅ Plus de validation - directement la logique métier
    return s.repo.GetFullMenu(ctx, user.MerchantID, lastUpdate)
}

func (s *MenuService) GetUnitsOfMeasures(ctx context.Context, user *auth.UserLoginRow) ([]UnitOfMeasure, error) {
    // ✅ Code simplifié
    return s.repo.GetUnitsOfMeasures(ctx, user.MerchantID)
}

func (s *MenuService) CreateProduct(ctx context.Context, user *auth.UserLoginRow, req *models.CreateProductRequest) (*models.ProductEntry, error) {
    // ✅ Validation métier directe
    if req.Name == "" {
        return nil, errors.New("product_name_required")
    }

    // Créer le produit
    productID, err := s.repo.InsertProduct(ctx, user.MerchantID, req)
    if err != nil {
        return nil, err
    }

    // Synchroniser avec Deliveroo/Uber si activé
    if user.DeliverooActive {
        go s.deliverooClient.SyncProduct(ctx, productID)
    }
    if user.UberEatsActive {
        go s.uberClient.SyncProduct(ctx, productID)
    }

    return s.repo.GetProductByID(ctx, productID)
}
```

## 📊 Statistiques de la migration

### Lignes de code supprimées
```
MenuService avant  : ~450 lignes
MenuService après  : ~320 lignes
Réduction          : 130 lignes (-29%)
```

### Lignes par méthode (moyenne)
```
Avant : ~34 lignes/méthode (avec validation)
Après : ~25 lignes/méthode (sans validation)
Gain  : 9 lignes/méthode
```

### Appels GetUserByToken supprimés
```
GetMenu()                    ❌
GetUnitsOfMeasures()         ❌
GetAttributes()              ❌
CreateCategory()             ❌
UpdateCategory()             ❌
DeleteCategory()             ❌
CreateProduct()              ❌
UpdateProduct()              ❌
DeleteProduct()              ❌
CreateComponent()            ❌
UpdateComponent()            ❌
DeleteComponent()            ❌
UpdateProductAllergens()     ❌
UpdateProductTags()          ❌

Total : 14 appels supprimés
```

## ✅ Checklist de migration du module Menu

- [x] Créer le middleware d'auth
- [x] Ajouter la fonction `MustGetUser()`
- [ ] Modifier `routes.go` pour appliquer le middleware sur `/menu`
- [ ] Mettre à jour `MenuHandler` :
  - [ ] `GetMenu()`
  - [ ] `GetUnitsOfMeasures()`
  - [ ] `GetAttributes()`
  - [ ] `CreateCategory()`
  - [ ] `UpdateCategory()`
  - [ ] `DeleteCategory()`
  - [ ] `CreateProduct()`
  - [ ] `UpdateProduct()`
  - [ ] `DeleteProduct()`
  - [ ] `CreateComponent()`
  - [ ] `UpdateComponent()`
  - [ ] `DeleteComponent()`
  - [ ] `UpdateProductAllergens()`
  - [ ] `UpdateProductTags()`
- [ ] Mettre à jour `MenuService` :
  - [ ] Supprimer `userRepo` du struct
  - [ ] Modifier le constructeur `NewMenuService()`
  - [ ] Changer signatures : `token string` → `user *auth.UserLoginRow`
  - [ ] Supprimer tous les appels à `GetUserByToken()`
- [ ] Mettre à jour `cmd/api/routes.go` pour retirer `authService` du `NewMenuService()`
- [ ] Tester tous les endpoints `/menu/*`
- [ ] Vérifier les logs (pas d'erreurs d'auth)

## 🧪 Tests

### Test manuel avec curl
```bash
# Sans token (doit échouer)
curl http://localhost:8080/menu

# Avec token valide
curl -H "Authorization: Bearer your-token-here" http://localhost:8080/menu

# Avec token invalide (doit échouer)
curl -H "Authorization: Bearer invalid-token" http://localhost:8080/menu
```

### Test unitaire du middleware
```go
func TestAuthMiddleware(t *testing.T) {
    // Mock repo
    mockRepo := &mockAuthRepo{}
    authMiddleware := middleware.Auth(mockRepo)

    // Handler de test
    handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        user := middleware.GetUser(r)
        if user == nil {
            t.Fatal("User should not be nil")
        }
        w.WriteHeader(http.StatusOK)
    }))

    // Requête avec token valide
    req := httptest.NewRequest("GET", "/test", nil)
    req.Header.Set("Authorization", "Bearer valid-token")
    w := httptest.NewRecorder()

    handler.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected 200, got %d", w.Code)
    }
}
```

## 🎯 Prochaines étapes

1. **Appliquer la migration au module Menu**
2. **Reproduire sur les autres modules** :
   - `orders`
   - `pos`
   - `customers`
   - `cash_registers`
   - `bookings`
   - `delivery_sessions`
   - `locations`
   - `stocks`
   - `user_services`

3. **Nettoyage final** :
   - Supprimer `helpers.ExtractToken()` si plus utilisé
   - Documenter les routes publiques vs protégées
   - Mettre à jour les tests d'intégration

---

**💡 Conseil** : Migrer un module complet à la fois, tester, puis passer au suivant. Ne pas tout migrer d'un coup !
