# Phase 2: Backend - Modifications Détaillées

**Durée:** 5-7 jours  
**Impacte:** Services, Repositories, Models, Handlers

---

## 📐 Architecture Changements

### Avant (Mono-Compte)
```
GET /integrations/uber-eats?merchant_id=XXX
→ Retourne UN compte (ou null)
```

### Après (Multi-Account)
```
GET /integrations/uber-eats?merchant_id=XXX
→ Retourne TOUS les comptes du merchant (liste)

GET /integrations/uber-eats?merchant_id=XXX&store_id=YYY
→ Retourne UN compte spécifique
```

---

## 🔄 Models Go

### Avant (integrations/models.go)
```go
type UberEatsIntegration struct {
    MerchantID              string
    StoreID                 string
    BearerToken             string
    RefreshToken            string
    BearerTokenExpirationDate *time.Time
    CommissionRate          *int
    AutoAcceptOrders        *bool
    EstimatedPreparationTime *string
    Enabled                 bool
}
```

### Après (ubereats/models.go)

```go
// Représentation d'un compte Uber Eats
type Account struct {
    MerchantID              string     `gorm:"primaryKey" json:"merchant_id"`
    StoreID                 string     `gorm:"primaryKey" json:"store_id"`
    BearerToken             string     `json:"bearer_token"`
    RefreshToken            string     `json:"refresh_token"`
    BearerTokenExpirationDate *time.Time `json:"bearer_token_expiration_date"`
    CommissionRate          *int       `json:"commission_rate"`
    AutoAcceptOrders        *bool      `json:"auto_accept_orders"`
    EstimatedPreparationTime *string   `json:"estimated_preparation_time"`
    Enabled                 bool       `json:"enabled"`
    ClosedUntil             *time.Time `json:"closed_until"`
    DelayUntil              *time.Time `json:"delay_until"`
    DelayDuration           *int       `json:"delay_duration"`
    UnlinkDate              *time.Time `json:"unlink_date"`
    LastSync                *time.Time `json:"last_sync"`
    SyncedItems             int        `json:"synced_items"`
    CreatedAt               time.Time  `json:"created_at"`
    UpdatedAt               time.Time  `json:"updated_at"`
}

// Réponse API (1 ou plusieurs comptes)
type Integration struct {
    MerchantID      string     `json:"merchant_id"`
    Accounts        []Account  `json:"accounts"`
    PrimaryStoreID  string     `json:"primary_store_id"` // Premier inséré
}

// Pour les endpoints qui retournent un seul compte
type AccountResponse = Account
```

### Idem pour Deliveroo

```go
type Account struct {
    MerchantID          string     `gorm:"primaryKey" json:"merchant_id"`
    LocationID          string     `gorm:"primaryKey" json:"location_id"`
    BrandID             string     `json:"brand_id"`
    CommissionRate      *int       `json:"commission_rate"`
    PreparationTimeMin  *int       `json:"preparation_time_minutes"`
    AutoAcceptOrders    *bool      `json:"auto_accept_orders"`
    Enabled             bool       `json:"enabled"`
    LastSync            *time.Time `json:"last_sync"`
    SyncedItems         int        `json:"synced_items"`
    CreatedAt           time.Time  `json:"created_at"`
    UpdatedAt           time.Time  `json:"updated_at"`
}

type Integration struct {
    MerchantID        string     `json:"merchant_id"`
    Accounts          []Account  `json:"accounts"`
    PrimaryLocationID string     `json:"primary_location_id"`
}
```

---

## 📦 Repositories

### ubereats/repository.go

#### NOUVELLE Méthode

```go
// GetAccountsByMerchant retourne tous les comptes d'un merchant
func (r *Repository) GetAccountsByMerchant(
    ctx context.Context,
    merchantID string,
) ([]Account, error) {
    var accounts []Account
    err := r.db.WithContext(ctx).
        Where("merchant_id = ?", merchantID).
        Order("created_at ASC"). // Premier inséré = primaire
        Find(&accounts).Error
    return accounts, err
}
```

#### MODIFIÉE (Existante)

```go
// GetByMerchantID - maintenant retourne une liste (mais optionnel)
// À DEPRECIER - utiliser GetAccountsByMerchant() à la place
func (r *Repository) GetByMerchantID(
    ctx context.Context,
    merchantID string,
) (*Account, error) {
    // Pour backward compatibility, retourner le premier (primaire)
    var account Account
    err := r.db.WithContext(ctx).
        Where("merchant_id = ?", merchantID).
        Order("created_at ASC").
        First(&account).Error
    return &account, err
}

// GetByStoreID - retrouver via store_id (utilisé par webhooks)
// Aucun changement logique - fonctionne toujours
func (r *Repository) GetByStoreID(
    ctx context.Context,
    storeID string,
) (*Account, error) {
    var account Account
    err := r.db.WithContext(ctx).
        Where("store_id = ?", storeID).
        First(&account).Error
    return &account, err
}

// GetMerchantIDFromStoreID - retrouver merchant à partir de store_id
// CRITIQUE pour les webhooks - aucun changement
func (r *Repository) GetMerchantIDFromStoreID(
    ctx context.Context,
    storeID string,
) (string, error) {
    var merchantID string
    err := r.db.WithContext(ctx).
        Model(&Account{}).
        Where("store_id = ?", storeID).
        Select("merchant_id").
        Row().
        Scan(&merchantID)
    return merchantID, err
}
```

---

## 🎯 Services

### ubereats/service.go

#### NOUVELLE Méthode

```go
// GetIntegration retourne tous les comptes du merchant
func (s *Service) GetIntegration(
    ctx context.Context,
    merchantID string,
) (*Integration, error) {
    accounts, err := s.repo.GetAccountsByMerchant(ctx, merchantID)
    if err != nil {
        return nil, err
    }

    if len(accounts) == 0 {
        return nil, fmt.Errorf("no integration found for merchant %s", merchantID)
    }

    // Le premier inséré est le compte primaire
    return &Integration{
        MerchantID:     merchantID,
        Accounts:       accounts,
        PrimaryStoreID: accounts[0].StoreID,
    }, nil
}
```

#### MODIFIÉE: SyncMenu avec Compte Optionnel

```go
// SyncMenu synce le menu avec Uber Eats
// - Si storeID fourni: syncer ce compte spécifique
// - Si NULL: syncer le compte primaire
func (s *Service) SyncMenu(
    ctx context.Context,
    merchantID string,
    storeID *string,
) error {
    // Déterminer le compte à syncer
    var targetStoreID string
    
    if storeID != nil && *storeID != "" {
        // Vérifier que ce store_id appartient au merchant
        account, err := s.repo.GetByStoreID(ctx, *storeID)
        if err != nil {
            return fmt.Errorf("store not found: %w", err)
        }
        if account.MerchantID != merchantID {
            return fmt.Errorf("store_id does not belong to merchant")
        }
        targetStoreID = *storeID
    } else {
        // Utiliser le compte primaire
        integration, err := s.GetIntegration(ctx, merchantID)
        if err != nil {
            return err
        }
        targetStoreID = integration.PrimaryStoreID
    }

    // Récupérer le compte et faire la sync
    account, err := s.repo.GetByStoreID(ctx, targetStoreID)
    if err != nil {
        return err
    }

    // Logique de sync existante
    return s.syncMenuInternal(ctx, account)
}

// Internal sync logic (refactorisé depuis l'existant)
func (s *Service) syncMenuInternal(ctx context.Context, account *Account) error {
    // ... logique actuelle de sync ...
}
```

#### EXISTANT: Webhooks - AUCUN CHANGEMENT

```go
// HandleOrderNotification traite les notifications de commandes Uber
// La logique reste EXACTEMENT identique
// Les webhooks retrouvent le merchant via store_id
func (s *Service) HandleOrderNotification(
    ctx context.Context,
    event *OrderNotificationEvent,
) error {
    // Étape 1: Retrouver le merchant via store_id
    merchantID, err := s.repo.GetMerchantIDFromStoreID(ctx, event.Meta.UserID)
    if err != nil {
        return err
    }

    // Étape 2: Retrouver le compte (pour le token)
    account, err := s.repo.GetByStoreID(ctx, event.Meta.UserID)
    if err != nil {
        return err
    }

    // Étape 3: Créer la commande
    // (la commande aura automatiquement store_id=account.StoreID)
    return s.createOrder(ctx, merchantID, account, event)
}
```

---

## 🔌 Handlers

### ubereats/handler.go

#### MODIFIÉE: GetIntegration

```go
func (h *Handler) GetIntegration(w http.ResponseWriter, r *http.Request) {
    merchantID := r.URL.Query().Get("merchant_id")
    storeID := r.URL.Query().Get("store_id")

    // Si store_id fourni: retourner ce compte spécifique
    if storeID != "" {
        account, err := h.service.repo.GetByStoreID(r.Context(), storeID)
        if err != nil {
            http.Error(w, "Account not found", http.StatusNotFound)
            return
        }

        // Vérifier que le compte appartient au merchant
        if account.MerchantID != merchantID {
            http.Error(w, "Unauthorized", http.StatusForbidden)
            return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(account)
        return
    }

    // Sinon: retourner TOUS les comptes du merchant
    integration, err := h.service.GetIntegration(r.Context(), merchantID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(integration)
}
```

#### MODIFIÉE: SyncMenu

```go
func (h *Handler) SyncMenu(w http.ResponseWriter, r *http.Request) {
    merchantID := r.URL.Query().Get("merchant_id")
    storeID := r.URL.Query().Get("store_id") // Optionnel

    var storeIDPtr *string
    if storeID != "" {
        storeIDPtr = &storeID
    }

    err := h.service.SyncMenu(r.Context(), merchantID, storeIDPtr)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status": "syncing",
        "message": "Menu sync started",
    })
}
```

#### EXISTANT: Webhooks - AUCUN CHANGEMENT

```go
func (h *Handler) PostWebhook(w http.ResponseWriter, r *http.Request) {
    // Exactement comme avant
    var event OrderNotificationEvent
    if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    if err := h.service.HandleOrderNotification(r.Context(), &event); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
}
```

---

## 🎁 Bonus: Backward Compatibility

Pour faciliter la migration, vous pouvez garder les anciens endpoints en deprecated:

```go
// GET /integrations/uber-eats (DEPRECATED - utiliser avec store_id optionnel)
// Retourne la liste des comptes pour backward compatibility
func (h *Handler) GetIntegrationLegacy(w http.ResponseWriter, r *http.Request) {
    merchantID := r.URL.Query().Get("merchant_id")

    integration, err := h.service.GetIntegration(r.Context(), merchantID)
    if err != nil {
        // Pour backward compat: retourner un seul compte (le primaire)
        account, err := h.service.repo.GetByMerchantID(r.Context(), merchantID)
        if err != nil {
            http.Error(w, "Not found", http.StatusNotFound)
            return
        }
        json.NewEncoder(w).Encode(account)
        return
    }

    json.NewEncoder(w).Encode(integration)
}
```

---

## 📝 Checklist Phase 2 Backend

- [ ] Models créés (Account, Integration)
- [ ] Repositories modifiés (GetAccountsByMerchant + existants)
- [ ] Services modifiés (GetIntegration + paramètres optionnels)
- [ ] Handlers modifiés (support store_id optionnel)
- [ ] Tests unitaires pour les repositories
- [ ] Tests unitaires pour les services
- [ ] Tests d'intégration (webhooks fonctionnent toujours)
- [ ] Vérification: aucun impact sur les webhooks
- [ ] Documentation du code mise à jour
