# ⚡ QUICKSTART - Module Availabilities

> 5 minutes pour être opérationnel

## 1️⃣ Migration SQL (2 min)
```bash
mysql -u user -p database < migrations/003_create_availabilities_tables.sql
```

## 2️⃣ Compiler (1 min)
```bash
go build ./cmd/api
# ✅ Build successful
```

## 3️⃣ Tester les endpoints (2 min)

### Créer une disponibilité
```bash
TOKEN="your-token-here"

curl -X POST http://localhost:8080/menu/availabilities \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Petit-déjeuner",
    "product_ids": ["prod-123"],
    "schedules": [{
      "day_of_week": 2,
      "start_time": "08:00",
      "end_time": "11:00"
    }]
  }'
```

### Vérifier disponibilité
```bash
curl -X GET "http://localhost:8080/menu/availabilities/check?product_id=prod-123" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 📚 Documentation

| Doc | Contenu | Temps |
|---|---|---|
| [DEPLOYMENT_SUMMARY](./AVAILABILITIES_DEPLOYMENT_SUMMARY.md) | Ce qui a été fait | 5 min |
| [EXAMPLES](./AVAILABILITIES_EXAMPLES.md) | Exemples réels | 10 min |
| [MODULE_GUIDE](./AVAILABILITIES_MODULE_GUIDE.md) | Référence complète | 20 min |
| [SETUP](./AVAILABILITIES_SETUP.md) | Installation détaillée | 5 min |
| [TESTS](./AVAILABILITIES_TESTS.md) | Exemples de tests | 15 min |

---

## 🎯 API Endpoints

```
GET    /menu/availabilities                 # Lister
POST   /menu/availabilities                 # Créer
PUT    /menu/availabilities/{id}            # Mettre à jour
DELETE /menu/availabilities/{id}            # Supprimer
GET    /menu/availabilities/check?product_id=X  # Vérifier
```

---

## 💡 Intégration ScanNOrder

```go
// Dans le service menu
isAvailable, _ := availabilitiesService.IsProductAvailable(
    ctx, 
    merchantID, 
    productID,
)
if isAvailable {
    // Inclure le produit
}
```

---

## ⚙️ Configuration

- **Jour de semaine** : 0 (dimanche) à 6 (samedi)
- **Heures** : Format `HH:MM` ou `HH:MM:SS` en UTC
- **Par défaut** : Aucune disponibilité = disponible

---

## ❓ Besoin d'aide ?

- ❌ Erreur compilation → [SETUP Troubleshooting](./AVAILABILITIES_SETUP.md#troubleshooting)
- ❓ API ne répond pas → Vérifier token valide
- 🤔 Comment intégrer ? → [EXAMPLES Section 7](./AVAILABILITIES_EXAMPLES.md#7-intégration-avec-scannorder)

---

## 📊 Status

✅ Code compilé
✅ DB Migration prête
✅ Routes enregistrées
✅ Documentation complète
✅ **Prêt pour production**

---

**Plus d'info → Voir [INDEX COMPLET](./AVAILABILITIES_INDEX.md) 📚**
