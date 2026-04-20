# 🚀 Guide d'installation - Module Availabilities

## Prérequis

- Go 1.18+
- MySQL 5.7+
- API Welloresto en cours d'exécution

---

## ✅ Étape 1 : Exécuter la migration SQL

### Option A : Ligne de commande
```bash
# Naviguer vers le répertoire du projet
cd /path/to/ib-welloresto-api

# Exécuter la migration
mysql -h localhost -u user -p database < migrations/003_create_availabilities_tables.sql

# Vérifier la création des tables
mysql -u user -p database -e "SHOW TABLES LIKE 'availabilities%';"
```

### Option B : Client MySQL
```sql
-- Copier/coller le contenu de migrations/003_create_availabilities_tables.sql
-- dans votre client MySQL et exécuter
```

### Vérification
```sql
-- Ces 3 tables doivent exister:
SELECT TABLE_NAME FROM information_schema.TABLES 
WHERE TABLE_SCHEMA = 'your_database' 
AND TABLE_NAME LIKE 'availabilities%';

-- Résultat attendu:
-- availabilities
-- availabilities_products  
-- availabilities_schedules
```

---

## ✅ Étape 2 : Vérifier la compilation

```bash
# Naviguer vers le dossier du projet
cd /path/to/ib-welloresto-api

# Compiler
go build ./cmd/api

# ✅ Si pas d'erreur, c'est bon !
# ❌ Si erreur, vérifier les imports dans cmd/api/routes.go
```

---

## ✅ Étape 3 : Démarrer l'API

```bash
# En développement
go run ./cmd/api/main.go

# Ou avec le binaire compilé
./cmd/api
```

---

## ✅ Étape 4 : Tester les endpoints

### Obtenir un token d'authentification
```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "password"}'

# Récupérer le token dans la réponse
# Utiliser comme: Authorization: Bearer <TOKEN>
```

### Créer une disponibilité
```bash
curl -X POST http://localhost:8080/menu/availabilities \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Availability",
    "product_ids": ["550e8400-e29b-41d4-a716-446655440001"],
    "schedules": [
      {
        "day_of_week": 2,
        "start_time": "08:00",
        "end_time": "11:00"
      }
    ]
  }'

# Réponse attendue: 201 Created
```

### Lister les disponibilités
```bash
curl -X GET http://localhost:8080/menu/availabilities \
  -H "Authorization: Bearer YOUR_TOKEN"

# Réponse attendue: 200 OK avec liste
```

### Vérifier la disponibilité d'un produit
```bash
curl -X GET "http://localhost:8080/menu/availabilities/check?product_id=550e8400-e29b-41d4-a716-446655440001" \
  -H "Authorization: Bearer YOUR_TOKEN"

# Réponse attendue: 200 OK avec {"is_available": true/false}
```

---

## 📋 Checklist d'installation

- [ ] Migration SQL exécutée
- [ ] 3 tables créées en base de données
- [ ] Code compilé sans erreur
- [ ] API démarre sans erreur
- [ ] GET /menu/availabilities retourne 200
- [ ] POST /menu/availabilities crée une disponibilité
- [ ] GET /menu/availabilities/check retourne is_available

---

## 🐛 Troubleshooting

### Erreur : "table 'availabilities' doesn't exist"
**Solution :** Exécuter la migration SQL (Étape 1)

### Erreur : "undefined: availabilitiesModule"
**Solution :** Vérifier que l'import est présent dans cmd/api/routes.go ligne ~30

### Erreur : "compilation error"
**Solution :** Exécuter `go mod tidy` puis `go build ./cmd/api`

### Les endpoints retournent 404
**Solution :** Vérifier que l'API a bien démarré et rechargé les routes

### Token invalide (401)
**Solution :** Vérifier que vous utilisiez un token valide et non expiré

---

## 🔧 Configuration optionnelle

### Modifier les logs
Dans `cmd/api/routes.go`, vous pouvez ajuster le niveau de log:
```go
r.Use(middleware.LoggingMiddleware(log))
```

### Modifier les timeouts
Dans le service `service.go`, vous pouvez ajuster:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

---

## 📚 Documentation complète

Consulter les fichiers de documentation pour plus de détails:
- `docs/AVAILABILITIES_MODULE_GUIDE.md` - Guide complet
- `docs/AVAILABILITIES_EXAMPLES.md` - Exemples pratiques
- `internal/modules/availabilities/README.md` - Quick reference

---

## ✅ Prochaines étapes

### 1. Intégrer avec ScanNOrder
```go
// Dans scannorder service
isAvailable, _ := availabilitiesService.IsProductAvailable(ctx, merchantID, productID)
```

### 2. Créer des disponibilités via le dashboard
- Utiliser les endpoints POST/PUT pour gérer les créneaux
- Filtrer automatiquement le menu selon l'heure

### 3. Monitorer
- Logger les disponibilités vérifiées
- Ajouter des métriques (nombre de produits filtrés, etc.)

---

## 📞 Support

Pour toute question sur:
- **Architecture** → Voir `AVAILABILITIES_MODULE_GUIDE.md`
- **API endpoints** → Voir `AVAILABILITIES_EXAMPLES.md`
- **Code** → Consulter les commentaires en ligne

---

**Installation complète estimée à < 5 minutes ! ⚡**
