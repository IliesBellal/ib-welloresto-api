# Module Availabilities - Guide d'implémentation

## Vue d'ensemble

Le module **Availabilities** (Disponibilités) permet de restreindre l'achat de certains produits à des jours et des créneaux horaires précis (ex: "Petit-déjeuner" disponible de 8h à 11h).

### Cas d'usage
- Restreindre les produits de petit-déjeuner à 8h-11h
- Limiter les menus déjeuner à 12h-14h
- Créer des créneaux Happy Hour (17h-19h)
- Gérer les spécialités saisonnières avec des horaires spécifiques

---

## Architecture

### Pattern : Handler → Service → Repository

```
Handler (API endpoints)
    ↓
Service (Business logic + validation)
    ↓
Repository (SQL queries)
```

### Tables de base de données

#### `availabilities`
Table principale contenant les métadonnées des disponibilités.

```sql
CREATE TABLE availabilities (
    availability_id CHAR(36) PRIMARY KEY,
    merchant_id CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled INT DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_merchant_id (merchant_id),
    INDEX idx_enabled (enabled),
    FOREIGN KEY (merchant_id) REFERENCES merchants(merchant_id)
);
```

#### `availabilities_products`
Table de liaison (many-to-many) entre les disponibilités et les produits.

```sql
CREATE TABLE availabilities_products (
    availability_product_id CHAR(36) PRIMARY KEY,
    availability_id CHAR(36) NOT NULL,
    product_id CHAR(36) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_availability_product (availability_id, product_id),
    FOREIGN KEY (availability_id) REFERENCES availabilities(availability_id) ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(product_id) ON DELETE CASCADE
);
```

#### `availabilities_schedules`
Table définissant les créneaux horaires pour chaque disponibilité.

```sql
CREATE TABLE availabilities_schedules (
    schedule_id CHAR(36) PRIMARY KEY,
    availability_id CHAR(36) NOT NULL,
    day_of_week INT NOT NULL,              -- 1=Lundi, ..., 7=Dimanche
    start_time TIME NOT NULL,               -- Format: HH:MM:SS
    end_time TIME NOT NULL,                 -- Format: HH:MM:SS
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (availability_id) REFERENCES availabilities(availability_id) ON DELETE CASCADE
);
```

---

## Endpoints API

### 1. Lister les disponibilités
```http
GET /menu/availabilities
Authorization: Bearer <token>
```

**Réponse (200):**
```json
{
  "availabilities": [
    {
      "availability_id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "Petit-déjeuner",
      "description": "Disponible le matin",
      "enabled": 1,
      "created_at": "2026-04-20T08:00:00Z",
      "updated_at": "2026-04-20T08:00:00Z",
      "product_ids": [
        "prod-123",
        "prod-456"
      ],
      "schedules": [
        {
          "schedule_id": "sch-789",
          "availability_id": "123e4567-e89b-12d3-a456-426614174000",
          "day_of_week": 2,  // Lundi
          "start_time": "08:00:00",
          "end_time": "11:00:00",
          "created_at": "2026-04-20T08:00:00Z",
          "updated_at": "2026-04-20T08:00:00Z"
        }
      ]
    }
  ]
}
```

### 2. Créer une disponibilité
```http
POST /menu/availabilities
Authorization: Bearer <token>
Content-Type: application/json
```

**Payload:**
```json
{
  "name": "Petit-déjeuner",
  "description": "Disponible le matin",
  "product_ids": [
    "prod-uuid-1",
    "prod-uuid-2"
  ],
  "schedules": [
    {
      "day_of_week": 2,  // Lundi
      "start_time": "08:00",
      "end_time": "11:00"
    },
    {
      "day_of_week": 3,  // Mardi
      "start_time": "08:00",
      "end_time": "11:00"
    }
  ]
}
```

**Réponse (201):**
```json
{
  "availability_id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Petit-déjeuner",
  "description": "Disponible le matin",
  "enabled": 1,
  "created_at": "2026-04-20T08:00:00Z",
  "updated_at": "2026-04-20T08:00:00Z",
  "product_ids": ["prod-uuid-1", "prod-uuid-2"],
  "schedules": [...]
}
```

### 3. Mettre à jour une disponibilité
```http
PUT /menu/availabilities/{id}
Authorization: Bearer <token>
Content-Type: application/json
```

**Payload:** Même format que la création

**Réponse (200):** Disponibilité mise à jour

### 4. Supprimer une disponibilité
```http
DELETE /menu/availabilities/{id}
Authorization: Bearer <token>
```

**Réponse (200):**
```json
{
  "status": "ok"
}
```

### 5. Vérifier la disponibilité d'un produit
```http
GET /menu/availabilities/check?product_id=<product_uuid>
Authorization: Bearer <token>
```

**Réponse (200):**
```json
{
  "is_available": true
}
```

---

## Logique de Validation

### IsProductAvailable(productID, merchantID)

La fonction vérifie si un produit est actuellement disponible selon l'heure UTC et le jour de la semaine.

#### Règles
1. **Pas de disponibilité définie** → Produit disponible par défaut
2. **Disponibilités définies** → Vérifier si l'heure actuelle (UTC) et le jour de la semaine correspondent à au moins un créneau

#### Implémentation
```go
func (s *AvailabilitiesService) IsProductAvailable(ctx context.Context, merchantID, productID string) (bool, error) {
    // 1. Récupérer les disponibilités pour ce produit
    availabilities, err := s.availabilitiesRepo.GetAvailabilitiesForProduct(ctx, merchantID, productID)
    
    // 2. Si aucune disponibilité, retourner true (disponible par défaut)
    if len(availabilities) == 0 {
        return true, nil
    }
    
    // 3. Récupérer l'heure et le jour actuels (UTC)
    now := time.Now().UTC()
    currentTime := now.Format("15:04:05")
    currentDayOfWeek := getDayOfWeek(now)
    
    // 4. Vérifier si correspondance avec au moins un créneau
    for _, availability := range availabilities {
        for _, schedule := range availability.Schedules {
            if schedule.DayOfWeek == currentDayOfWeek && 
               currentTime >= schedule.StartTime && 
               currentTime <= schedule.EndTime {
                return true, nil
            }
        }
    }
    
    // 5. Aucune correspondance
    return false, nil
}
```

#### Jour de la semaine
- 1 = Lundi
- 2 = Mardi
- 3 = Mercredi
- 4 = Jeudi
- 5 = Vendredi
- 6 = Samedi
- 7 = Dimanche

#### Format d'heure
- Format accepté en entrée: `HH:MM` ou `HH:MM:SS`
- Normalisé en base de données: `HH:MM:SS`

---

## Intégration avec ScanNOrder

### Filtrage du menu
Pour filtrer les produits du menu ScanNOrder en fonction des disponibilités:

```go
// Dans le service menu
func (s *MenuService) GetMenuForCustomer(ctx context.Context, merchantID string) ([]Product, error) {
    products, err := s.getBaseMenu(ctx, merchantID)
    if err != nil {
        return nil, err
    }
    
    // Filtrer par disponibilité
    var availableProducts []Product
    for _, product := range products {
        isAvailable, err := s.availabilitiesService.IsProductAvailable(ctx, merchantID, product.ID)
        if err == nil && isAvailable {
            availableProducts = append(availableProducts, product)
        }
    }
    
    return availableProducts, nil
}
```

### Intégration recommandée
1. **Dashboard** (back-office) : Gérer les disponibilités
2. **ScanNOrder** (client) : Filtrer automatiquement selon l'heure
3. **Notifications** : Alerter les clients quand un produit devient disponible

---

## Gestion des transactions

### Pattern atomique pour Create/Update

Les opérations d'écriture utilisent des transactions pour garantir l'intégrité des données:

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return nil, err
}
defer tx.Rollback()

// Injection de la transaction dans le contexte
ctx = dbutils.InjectTx(ctx, tx)

// Les opérations SQL utilisent automatiquement la transaction
// ...

if err = tx.Commit(); err != nil {
    return nil, err
}
```

---

## Format de validation

### Créneaux horaires valides
```json
{
  "day_of_week": 1,        // 0-6
  "start_time": "08:00",   // HH:MM ou HH:MM:SS
  "end_time": "11:00"      // HH:MM ou HH:MM:SS (doit être > start_time)
}
```

### Erreurs possibles
- `availability_name_required` : Le nom ne peut pas être vide
- `at_least_one_product_required` : Au moins un produit est obligatoire
- `at_least_one_schedule_required` : Au moins un créneau est obligatoire
- `invalid_day_of_week` : day_of_week doit être entre 0 et 6
- `invalid_time_format` : Format d'heure invalide
- `start_time_must_be_before_end_time` : start_time >= end_time
- `availability_not_found` : La disponibilité n'existe pas

---

## Suppression logique

Les disponibilités ne sont **jamais physiquement supprimées**. Elles sont marquées comme `enabled = 0`.

### Avantages
- Traçabilité complète
- Récupération possible en cas d'erreur
- Audit facilité

---

## Migration SQL

Fichier: `migrations/003_create_availabilities_tables.sql`

Exécutez la migration pour créer les tables:
```bash
mysql -u user -p database < migrations/003_create_availabilities_tables.sql
```

---

## Logging

- **Pas de logs manuels** dans les handlers (gérés par le middleware)
- Les erreurs de business logic sont retournées comme réponses HTTP
- Les erreurs techniques sont loggées au niveau du service/repository

---

## Exemple complet d'utilisation

### Créer une disponibilité "Petit-déjeuner"
```bash
POST /menu/availabilities
{
  "name": "Petit-déjeuner",
  "description": "Disponible le matin (08:00-11:00)",
  "product_ids": ["cafe-uuid", "croissant-uuid", "oeuf-uuid"],
  "schedules": [
    { "day_of_week": 2, "start_time": "08:00", "end_time": "11:00" },
    { "day_of_week": 3, "start_time": "08:00", "end_time": "11:00" },
    { "day_of_week": 4, "start_time": "08:00", "end_time": "11:00" },
    { "day_of_week": 5, "start_time": "08:00", "end_time": "11:00" },
    { "day_of_week": 6, "start_time": "08:00", "end_time": "11:00" }
  ]
}
```

### Vérifier la disponibilité
```bash
GET /menu/availabilities/check?product_id=cafe-uuid
# Si nous sommes lundi 9h → { "is_available": true }
# Si nous sommes lundi 12h → { "is_available": false }
```

### Filtrer le menu ScanNOrder
```bash
GET /scannorder/{merchant_slug}/menu
# Retourne uniquement les produits actuellement disponibles
```

---

## Notes importantes

1. **Les IDs sont des UUIDs** (CHAR(36))
2. **Toutes les heures sont en UTC**
3. **Les créneaux peuvent se chevaucher** (c'est prévu)
4. **La suppression est logique** (enabled = 0)
5. **Les transactions garantissent l'intégrité** des trois tables
6. **Les performances** : Les indexes sur day_of_week et time_range optimisent les requêtes

---

## Troubleshooting

### Les produits ne sont pas filtrés
- Vérifier que le jour_of_week est correct (1-7)
- Vérifier que l'heure UTC est dans la bonne plage
- Vérifier que enabled = 1

### Les créneaux ne correspondent pas
- Vérifier le format `HH:MM:SS`
- Vérifier que start_time < end_time
- Vérifier le jour de la semaine

### Erreur de transaction
- Vérifier que r.database n'est pas nil
- Vérifier la connexion à la base de données
- Vérifier les permissions SQL

---

## Fichiers du module

```
internal/modules/availabilities/
├── models.go         # Structures de données
├── repository.go     # Couche SQL
├── service.go        # Logique métier
├── handler.go        # Endpoints HTTP
```

Et la migration:
```
migrations/
└── 003_create_availabilities_tables.sql
```

---

**Module prêt à être utilisé dans le ScanNOrder pour filtrer le menu ! 🚀**
