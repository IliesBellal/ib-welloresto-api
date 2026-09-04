# Phase 1: Modifications Base de Données - Détails Complets

**Durée:** 1-2 jours  
**Environnement:** Local → Staging → Production

---

## 📋 Fichier de Migration

**Créer:** `migrations/done/XXX_multi_account_integration.sql`

### 1. Modifier Clés Primaires

```sql
-- ==========================================
-- UBER EATS: Supporter plusieurs comptes par merchant
-- ==========================================

-- Étape 1: Supprimer la contrainte PRIMARY KEY existante
ALTER TABLE integration_uber_eats
DROP CONSTRAINT integration_uber_eats_pkey;

-- Étape 2: Ajouter clé composite (merchant_id, store_id)
ALTER TABLE integration_uber_eats
ADD PRIMARY KEY (merchant_id, store_id);

-- Étape 3: Index pour les webhooks (retrouver merchant via store_id)
CREATE INDEX idx_uber_eats_store_id ON integration_uber_eats(store_id);

-- ==========================================
-- DELIVEROO: Supporter plusieurs comptes par merchant
-- ==========================================

ALTER TABLE integration_deliveroo
DROP CONSTRAINT integration_deliveroo_pkey;

ALTER TABLE integration_deliveroo
ADD PRIMARY KEY (merchant_id, location_id);

CREATE INDEX idx_deliveroo_location_id ON integration_deliveroo(location_id);
```

### 2. Ajouter store_id aux Mappings Produits

```sql
-- ==========================================
-- UBER EATS: Mappings produits par account
-- ==========================================

-- Ajouter colonne store_id
ALTER TABLE integration_uber_eats_products_mapping
ADD COLUMN store_id varchar(150);

-- Ajouter colonne location_id pour Deliveroo (dans sa table)
ALTER TABLE integration_deliveroo_products_mapping
ADD COLUMN location_id varchar(20);

-- Ajouter options_mapping
ALTER TABLE integration_uber_eats_options_mapping
ADD COLUMN store_id varchar(150);

ALTER TABLE integration_deliveroo_options_mapping
ADD COLUMN location_id varchar(20);

-- Ajouter attributes_mapping
ALTER TABLE integration_uber_eats_attributes_mapping
ADD COLUMN store_id varchar(150);

-- Ajouter components_mapping
ALTER TABLE integration_uber_eats_components_mapping
ADD COLUMN store_id varchar(150);

-- ==========================================
-- Créer INDEX pour les nouvelles clés composites
-- ==========================================

CREATE INDEX idx_uber_products_mapping ON integration_uber_eats_products_mapping(
    merchant_id, store_id, local_product_id
);

CREATE INDEX idx_deliveroo_products_mapping ON integration_deliveroo_products_mapping(
    merchant_id, location_id, local_product_id
);
```

### 3. Ajouter store_id/location_id à la Table Orders

```sql
-- ==========================================
-- ORDERS: Tracer l'origine de la commande par account
-- ==========================================

-- Ajouter colonne store_id (Uber Eats)
ALTER TABLE orders
ADD COLUMN store_id varchar(150);

-- Ajouter colonne location_id (Deliveroo)
ALTER TABLE orders
ADD COLUMN location_id varchar(20);

-- Index pour retrouver les commandes par account
CREATE INDEX idx_orders_store_id ON orders(merchant_id, store_id)
WHERE brand = 'UBER_EATS';

CREATE INDEX idx_orders_location_id ON orders(merchant_id, location_id)
WHERE brand = 'DELIVEROO';
```

---

## 🔄 Scripts de Migration des Données Existantes

### Remplir store_id dans les Mappings Existants

```sql
-- ==========================================
-- SCRIPT 1: Migrer les mappings Uber Eats existants
-- ==========================================

UPDATE integration_uber_eats_products_mapping m
SET store_id = (
    SELECT store_id FROM integration_uber_eats ue
    WHERE ue.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE store_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_uber_eats
);

UPDATE integration_uber_eats_options_mapping m
SET store_id = (
    SELECT store_id FROM integration_uber_eats ue
    WHERE ue.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE store_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_uber_eats
);

UPDATE integration_uber_eats_attributes_mapping m
SET store_id = (
    SELECT store_id FROM integration_uber_eats ue
    WHERE ue.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE store_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_uber_eats
);

UPDATE integration_uber_eats_components_mapping m
SET store_id = (
    SELECT store_id FROM integration_uber_eats ue
    WHERE ue.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE store_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_uber_eats
);

-- ==========================================
-- SCRIPT 2: Migrer les mappings Deliveroo existants
-- ==========================================

UPDATE integration_deliveroo_products_mapping m
SET location_id = (
    SELECT location_id FROM integration_deliveroo dr
    WHERE dr.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE location_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_deliveroo
);

UPDATE integration_deliveroo_options_mapping m
SET location_id = (
    SELECT location_id FROM integration_deliveroo dr
    WHERE dr.merchant_id = m.merchant_id
    LIMIT 1
)
WHERE location_id IS NULL AND merchant_id IN (
    SELECT DISTINCT merchant_id FROM integration_deliveroo
);
```

### Remplir store_id/location_id dans les Orders Existantes

```sql
-- ==========================================
-- SCRIPT 3: Associer les commandes Uber Eats à leur store_id
-- ==========================================

UPDATE orders o
SET store_id = (
    SELECT store_id FROM integration_uber_eats ue
    WHERE ue.merchant_id = o.merchant_id
    LIMIT 1
)
WHERE o.brand = 'UBER_EATS'
  AND store_id IS NULL
  AND merchant_id IN (
      SELECT DISTINCT merchant_id FROM integration_uber_eats
  );

-- ==========================================
-- SCRIPT 4: Associer les commandes Deliveroo à leur location_id
-- ==========================================

UPDATE orders o
SET location_id = (
    SELECT location_id FROM integration_deliveroo dr
    WHERE dr.merchant_id = o.merchant_id
    LIMIT 1
)
WHERE o.brand = 'DELIVEROO'
  AND location_id IS NULL
  AND merchant_id IN (
      SELECT DISTINCT merchant_id FROM integration_deliveroo
  );
```

---

## 🧪 Procédure de Test

### Étape 1: Local

```bash
# Appliquer la migration
psql -U postgres -d wello_resto -f migrations/done/XXX_multi_account_integration.sql

# Vérifier que les structures sont correctes
psql -U postgres -d wello_resto -c "
  \d integration_uber_eats
  \d integration_deliveroo
"

# Vérifier les données existantes
psql -U postgres -d wello_resto -c "
  SELECT merchant_id, store_id, enabled FROM integration_uber_eats LIMIT 5;
  SELECT COUNT(*) as mapping_count FROM integration_uber_eats_products_mapping WHERE store_id IS NOT NULL;
"
```

### Étape 2: Staging

```bash
# Faire un backup complet
pg_dump -U postgres wello_resto > backup_before_migration.sql

# Appliquer la migration
psql -U postgres -d wello_resto -f migrations/done/XXX_multi_account_integration.sql

# Tester les webhooks
# - Envoyer un webhook Uber Eats fictif
# - Vérifier qu'il retrouve le merchant_id correctement
# - Vérifier que la commande créée a store_id rempli

# Tester les queries existantes
# - GET /integrations/uber-eats?merchant_id=XXX
# - Vérifier que ça retourne la liste des comptes
```

### Étape 3: Rollback (Si besoin)

```bash
# Si quelque chose ne va pas, restaurer le backup
psql -U postgres -d wello_resto < backup_before_migration.sql
```

---

## ⚠️ Points Critiques à Vérifier

### 1. Contraintes d'Unicité

| Table | Avant | Après |
|-------|-------|-------|
| `integration_uber_eats` | `PK: merchant_id` | `PK: (merchant_id, store_id)` |
| `integration_deliveroo` | `PK: merchant_id` | `PK: (merchant_id, location_id)` |
| `integration_uber_eats_products_mapping` | `PK: (merchant_id, local_product_id)` | `PK: (merchant_id, store_id, local_product_id)` |

### 2. Foreign Keys

Vérifier qu'aucune FK ne pointe vers les clés primaires modifiées d'une manière incompatible.

```sql
-- Chercher les FKs
SELECT constraint_name, table_name, column_name
FROM information_schema.key_column_usage
WHERE table_name IN (
    'integration_uber_eats',
    'integration_deliveroo',
    'integration_uber_eats_products_mapping'
);
```

### 3. Vérification de Complétude

```sql
-- Après la migration, vérifier que TOUS les mappings ont store_id rempli
SELECT 
    'Uber products' as mapping_type,
    COUNT(*) as total,
    COUNT(CASE WHEN store_id IS NOT NULL THEN 1 END) as with_store_id,
    COUNT(CASE WHEN store_id IS NULL THEN 1 END) as missing
FROM integration_uber_eats_products_mapping

UNION ALL

SELECT 
    'Deliveroo products' as mapping_type,
    COUNT(*) as total,
    COUNT(CASE WHEN location_id IS NOT NULL THEN 1 END) as with_location_id,
    COUNT(CASE WHEN location_id IS NULL THEN 1 END) as missing
FROM integration_deliveroo_products_mapping;
```

---

## 📝 Checklist Phase 1

- [ ] Migration SQL créée et testée sur local
- [ ] Tous les scripts de migration des données exécutés
- [ ] Vérification de complétude: 0 mappings/orders avec store_id NULL
- [ ] Dry-run sur staging
- [ ] Webhooks testés sur staging
- [ ] Queries existantes testées
- [ ] Backup de production avant déploiement
- [ ] Déploiement en production pendant heures creuses
- [ ] Monitoring 24h après déploiement

---

## 🚀 Prochaines Phases

Une fois Phase 1 validée:
- **Phase 2:** Backend (adapter repositories, services, handlers)
- **Phase 3:** Frontend (adapter pages, ajouter select déroulant)
- **Phase 4:** Flutter (adapter models)
- **Phase 5:** Tests & Déploiement
