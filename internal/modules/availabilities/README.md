# Module Availabilities

Gestion des disponibilités produits basée sur les créneaux horaires (jour + heure).

## Fichiers

- **models.go** : Structures `Availability`, `AvailabilitySchedule`, DTOs
- **repository.go** : Requêtes SQL (CRUD atomique)
- **service.go** : Logique métier, validation, `IsProductAvailable()`
- **handler.go** : Endpoints HTTP (GET, POST, PUT, DELETE, CHECK)

## Endpoints

```
GET    /menu/availabilities                    # Lister
POST   /menu/availabilities                    # Créer
PUT    /menu/availabilities/{id}               # Mettre à jour
DELETE /menu/availabilities/{id}               # Supprimer
GET    /menu/availabilities/check?product_id=X # Vérifier disponibilité
```

## Logique clé

### IsProductAvailable(productID, merchantID)
1. Aucune disponibilité définie → produit disponible par défaut
2. Disponibilités définies → vérifier si heure UTC et jour_of_week correspondent
3. Retourner booléen

### Jours de la semaine
- 0 = Dimanche, 1 = Lundi, ..., 6 = Samedi

## Base de données

3 tables créées par `migrations/003_create_availabilities_tables.sql`:
- `availabilities` (métadonnées)
- `availabilities_products` (liaison many-to-many)
- `availabilities_schedules` (créneaux horaires)

## Intégration

Utilisé par ScanNOrder pour filtrer le menu selon les créneaux disponibles.

### Exemple
```go
isAvailable, err := availabilitiesService.IsProductAvailable(ctx, merchantID, productID)
if isAvailable {
    // Inclure le produit dans le menu
}
```

## Notes

- ✅ Architecture : Handler → Service → Repository
- ✅ Transactions atomiques (3 tables)
- ✅ Suppression logique (enabled = 0)
- ✅ IDs UUID (CHAR(36))
- ✅ Heures en UTC
- ✅ JSON tags en snake_case
- ✅ Pas de logs manuels (middleware)

---

**Voir `docs/AVAILABILITIES_MODULE_GUIDE.md` pour la documentation complète.**
