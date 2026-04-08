# Adaptation de la mise à jour de commande - Suivi des prix

## 📋 Résumé des modifications

Adaptation de l'endpoint `POST /orders/{order_id}/update` pour supporter le suivi des prix avec promotions en distinguant le prix de base (`base_price`) du prix final (`price`).

## 🔄 Flux de traitement

### Payload entrant
```json
{
  "order": {
    "products": [
      {
        "product_id": "prod_123",
        "quantity": 2,
        "price": 1000,           // Prix de base
        "discounted_price": 800   // Prix après promotion (optionnel)
      }
    ]
  }
}
```

### Stockage en base de données
- **`base_price`** : Stocke le prix du payload (`price`) → permet de suivre le prix de base
- **`price`** : Stocke le prix final :
  - Si `discounted_price` est fourni → utiliser `discounted_price`
  - Sinon → utiliser `price`

### Exemple de résultat
Pour l'exemple ci-dessus, en base de données :
- `base_price` = 1000 (prix original)
- `price` = 800 (prix après promotion appliquée)
- Différence = 200 (économie due à la promotion)

## 📝 Fichiers modifiés

### 1. **base_price colonne** 
- **Fichier** : `migrations/001_add_base_price_to_orderitems.sql`
- **Action** : Migration SQL pour ajouter la colonne `base_price` à la table `orderitems`

### 2. **Modèle OrderProductPayload**
- **Fichier** : `internal/models/create_order_models.go`
- Support du champ `discounted_price` (*int) déjà présent

### 3. **Modèle OrderItemInsert**
- **Fichier** : `internal/models/orders_model.go`
- Ajout des champs :
  - `BasePrice` : prix de base reçu du payload
  - `DiscountedPrice` : prix actualisé (optionnel)

### 4. **Repository - Fonction UpdateOrder**
- **Fichier** : `internal/modules/order_life_cycle/repository.go` (ligne ~1305)
- Modifications :
  - INSERT et UPDATE incluent `base_price`
  - Calcul du prix final : `finalPrice = discounted_price ?? price`
  - Paramètres : `p.Price` (base), `finalPrice` (final)

### 5. **Repository - Fonction insertOrderItems**
- **Fichier** : `internal/modules/order_life_cycle/repository.go` (ligne ~1911)
- Adaptations :
  - Calcul du prix final avant insertion
  - Passage de `BasePrice` et `DiscountedPrice` au modèle

### 6. **Repository - Fonction InsertOrderItem**
- **Fichier** : `internal/modules/order_life_cycle/repository.go` (ligne ~1942)
- INSERT modifié pour inclure `base_price` dans les colonnes

### 7. **Orders Fetcher - Query SELECT products**
- **Fichier** : `internal/modules/orders/orders_fetcher_builder.go` (ligne ~410)
- Changement : `p.price as base_price` → `oi.base_price`
- Récupère désormais le prix de base de l'item, pas du produit

## 🚀 Instructions de déploiement

### 1. Exécuter la migration SQL
```sql
-- Exécuter le fichier migration
mysql -u user -p database < migrations/001_add_base_price_to_orderitems.sql
```

### 2. (Optionnel) Remplir les valeurs existantes
Si vous avez des commandes existantes, vous pouvez peupler `base_price` :
```sql
UPDATE orderitems SET base_price = price WHERE base_price = 0;
```

### 3. Redéployer l'API
- Les modifications du code sont compatibles et n'ont pas d'impact breaking
- Les anciennes commandes sans `base_price` auront 0 par défaut
- Les nouvelles commandes auront les bonnes valeurs

## 📊 Suivi des promotions

Vous pouvez désormais analyser l'impact des promotions avec :
```sql
SELECT 
  order_id,
  product_id,
  quantity,
  base_price,
  price,
  (base_price - price) * quantity as discount_total
FROM orderitems
WHERE base_price > price
ORDER BY discount_total DESC;
```

## ✅ Validation

Les modifications couvrent :
- ✅ Création de commande (`CreateOrder` → `insertOrderItems`)
- ✅ Mise à jour de commande (`UpdateOrder`)
- ✅ Récupération de commande (SELECT inclut `base_price`)
- ✅ Stockage adéquat des prix (base vs actualisé)
