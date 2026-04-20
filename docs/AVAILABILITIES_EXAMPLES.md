# Exemples d'utilisation du module Availabilities

## 1. Créer une disponibilité "Petit-déjeuner"

### Requête cURL
```bash
curl -X POST http://localhost:8080/menu/availabilities \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Petit-déjeuner",
    "description": "Disponible le matin (8h-11h)",
    "product_ids": [
      "550e8400-e29b-41d4-a716-446655440001",
      "550e8400-e29b-41d4-a716-446655440002"
    ],
    "schedules": [
      {
        "day_of_week": 1,
        "start_time": "08:00",
        "end_time": "11:00"
      },
      {
        "day_of_week": 2,
        "start_time": "08:00",
        "end_time": "11:00"
      },
      {
        "day_of_week": 3,
        "start_time": "08:00",
        "end_time": "11:00"
      },
      {
        "day_of_week": 4,
        "start_time": "08:00",
        "end_time": "11:00"
      },
      {
        "day_of_week": 5,
        "start_time": "08:00",
        "end_time": "11:00"
      }
    ]
  }'
```

### Réponse (201)
```json
{
  "availability_id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Petit-déjeuner",
  "description": "Disponible le matin (8h-11h)",
  "enabled": 1,
  "created_at": "2026-04-20T08:00:00Z",
  "updated_at": "2026-04-20T08:00:00Z",
  "product_ids": [
    "550e8400-e29b-41d4-a716-446655440001",
    "550e8400-e29b-41d4-a716-446655440002"
  ],
  "schedules": [
    {
      "schedule_id": "sch-001",
      "availability_id": "123e4567-e89b-12d3-a456-426614174000",
      "day_of_week": 1,
      "start_time": "08:00:00",
      "end_time": "11:00:00",
      "created_at": "2026-04-20T08:00:00Z",
      "updated_at": "2026-04-20T08:00:00Z"
    }
  ]
}
```

---

## 2. Lister toutes les disponibilités

### Requête
```bash
curl -X GET http://localhost:8080/menu/availabilities \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Réponse (200)
```json
{
  "availabilities": [
    {
      "availability_id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "Petit-déjeuner",
      "description": "Disponible le matin (8h-11h)",
      "enabled": 1,
      "created_at": "2026-04-20T08:00:00Z",
      "updated_at": "2026-04-20T08:00:00Z",
      "product_ids": [...],
      "schedules": [...]
    },
    {
      "availability_id": "223e4567-e89b-12d3-a456-426614174000",
      "name": "Déjeuner",
      "description": "Disponible à midi",
      "enabled": 1,
      "created_at": "2026-04-20T10:00:00Z",
      "updated_at": "2026-04-20T10:00:00Z",
      "product_ids": [...],
      "schedules": [...]
    }
  ]
}
```

---

## 3. Mettre à jour une disponibilité

### Requête
```bash
curl -X PUT http://localhost:8080/menu/availabilities/123e4567-e89b-12d3-a456-426614174000 \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Petit-déjeuner Premium",
    "description": "Petit-déjeuner avec menu spécial",
    "product_ids": [
      "550e8400-e29b-41d4-a716-446655440001",
      "550e8400-e29b-41d4-a716-446655440003"
    ],
    "schedules": [
      {
        "day_of_week": 2,
        "start_time": "07:00",
        "end_time": "12:00"
      }
    ]
  }'
```

### Réponse (200)
```json
{
  "availability_id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Petit-déjeuner Premium",
  "description": "Petit-déjeuner avec menu spécial",
  "enabled": 1,
  "created_at": "2026-04-20T08:00:00Z",
  "updated_at": "2026-04-20T14:00:00Z",
  "product_ids": [
    "550e8400-e29b-41d4-a716-446655440001",
    "550e8400-e29b-41d4-a716-446655440003"
  ],
  "schedules": [...]
}
```

---

## 4. Supprimer une disponibilité

### Requête
```bash
curl -X DELETE http://localhost:8080/menu/availabilities/123e4567-e89b-12d3-a456-426614174000 \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Réponse (200)
```json
{
  "status": "ok"
}
```

---

## 5. Vérifier la disponibilité d'un produit

### Requête (lundi 9h)
```bash
curl -X GET "http://localhost:8080/menu/availabilities/check?product_id=550e8400-e29b-41d4-a716-446655440001" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Réponse (200) - Produit disponible
```json
{
  "is_available": true
}
```

### Réponse (200) - Produit non disponible
```json
{
  "is_available": false
}
```

---

## 6. Cas d'utilisation réels

### Cas 1 : Menu Petit-déjeuner/Déjeuner/Dîner

```json
{
  "availabilities": [
    {
      "name": "Petit-déjeuner",
      "product_ids": ["cafe-uuid", "croissant-uuid"],
      "schedules": [
        { "day_of_week": 1, "start_time": "06:00", "end_time": "11:00" },
        { "day_of_week": 2, "start_time": "06:00", "end_time": "11:00" },
        { "day_of_week": 3, "start_time": "06:00", "end_time": "11:00" },
        { "day_of_week": 4, "start_time": "06:00", "end_time": "11:00" },
        { "day_of_week": 5, "start_time": "06:00", "end_time": "11:00" },
        { "day_of_week": 6, "start_time": "07:00", "end_time": "12:00" },
        { "day_of_week": 0, "start_time": "07:00", "end_time": "12:00" }
      ]
    },
    {
      "name": "Déjeuner",
      "product_ids": ["pizza-uuid", "salade-uuid"],
      "schedules": [
        { "day_of_week": 1, "start_time": "11:30", "end_time": "14:30" },
        { "day_of_week": 2, "start_time": "11:30", "end_time": "14:30" },
        { "day_of_week": 4, "start_time": "11:30", "end_time": "14:30" },
        { "day_of_week": 5, "start_time": "11:30", "end_time": "14:30" },
        { "day_of_week": 6, "start_time": "11:30", "end_time": "14:30" }
      ]
    },
    {
      "name": "Dîner",
      "product_ids": ["burger-uuid", "poulet-uuid"],
      "schedules": [
        { "day_of_week": 2, "start_time": "18:00", "end_time": "23:00" },
        { "day_of_week": 3, "start_time": "18:00", "end_time": "23:00" },
        { "day_of_week": 4, "start_time": "18:00", "end_time": "23:00" },
        { "day_of_week": 5, "start_time": "18:00", "end_time": "23:00" },
        { "day_of_week": 6, "start_time": "18:00", "end_time": "00:00" },
        { "day_of_week": 7, "start_time": "18:00", "end_time": "00:00" }
      ]
    }
  ]
}
```

### Cas 2 : Menu Happy Hour

```json
{
  "name": "Happy Hour",
  "description": "Tarif réduit 17h-19h",
  "product_ids": ["cocktail-uuid", "biere-uuid"],
  "schedules": [
    { "day_of_week": 2, "start_time": "17:00", "end_time": "19:00" },
    { "day_of_week": 3, "start_time": "17:00", "end_time": "19:00" },
    { "day_of_week": 4, "start_time": "17:00", "end_time": "19:00" },
    { "day_of_week": 5, "start_time": "17:00", "end_time": "19:00" }
  ]
}
```

### Cas 3 : Menu Weekend

```json
{
  "name": "Menu Weekend",
  "description": "Menu spécial samedi et dimanche",
  "product_ids": ["brunch-uuid", "dessert-uuid"],
  "schedules": [
    { "day_of_week": 6, "start_time": "10:00", "end_time": "16:00" },
    { "day_of_week": 0, "start_time": "10:00", "end_time": "16:00" }
  ]
}
```

---

## 7. Intégration avec ScanNOrder

```go
package scannorder

import "welloresto-api/internal/modules/availabilities"

func (h *Handler) GetMenu(w http.ResponseWriter, r *http.Request) {
    merchantID := chi.URLParam(r, "merchant_slug")
    
    // Récupérer le menu complet
    products, _ := h.service.GetBaseMenu(ctx, merchantID)
    
    // Filtrer par disponibilité
    var availableProducts []Product
    for _, product := range products {
        isAvailable, err := h.availabilitiesService.IsProductAvailable(
            ctx, 
            merchantID, 
            product.ID,
        )
        if err == nil && isAvailable {
            availableProducts = append(availableProducts, product)
        }
    }
    
    // Retourner le menu filtré
    sendJSON(w, http.StatusOK, map[string]interface{}{
        "products": availableProducts,
    })
}
```

---

## Notes importantes

1. **Format d'heure** : Accepte `HH:MM` ou `HH:MM:SS`, stocké en `HH:MM:SS`
2. **Jour de la semaine** : 0 (dimanche) à 6 (samedi)
3. **Heure UTC** : Toutes les vérifications utilisent `time.Now().UTC()`
4. **Par défaut** : Si aucune disponibilité n'existe pour un produit, il est disponible
5. **Suppression logique** : Les disponibilités supprimées restent en base avec `enabled = 0`

---

**Besoin d'aide ? Consultez `AVAILABILITIES_MODULE_GUIDE.md`**
