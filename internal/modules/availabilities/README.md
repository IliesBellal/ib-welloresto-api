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

### GetUnavailableProductsAt(merchantID, at) / UnavailableProductsAt
Règle « liste blanche », une seule requête (`GetActiveProductSchedules`) :
1. Produit rattaché à aucune disponibilité active → disponible
2. Sinon → disponible seulement si `at`, exprimé dans le fuseau du merchant (`merchant.timezone`), tombe dans un de ses créneaux actifs
3. Disponibilité active = `enabled` + `available` + `availabilities_products.enabled` ; créneau actif = `enabled`

`IsProductAvailable` / `IsProductAvailableAt` appliquent la même règle.

### Listes vides (depuis 2026-09-27)
Une disponibilité peut n'avoir **aucun produit** (elle ne restreint rien) ou
**aucun créneau** (active, elle masque ses produits en permanence). Au PATCH,
pour `product_ids` et `schedules` : clé absente = inchangé, `null` ou `[]` =
liste vidée. Voir `docs/AVAILABILITIES_EMPTY_LISTS.md`.

### Stockage en heure locale
Les créneaux sont des heures de mur du merchant (« 6h–11h le lundi », été comme
hiver) : stockés tels que saisis dans le back-office, renvoyés tels quels, sans
aucune conversion. Même convention que les horaires d'ouverture et les
promotions programmées. `validateSchedules` impose début < fin : un créneau ne
passe jamais minuit.

### Jours de la semaine
- 1 = Lundi, ..., 7 = Dimanche

## Base de données

3 tables créées par `migrations/003_create_availabilities_tables.sql`:
- `availabilities` (métadonnées)
- `availabilities_products` (liaison many-to-many)
- `availabilities_schedules` (créneaux horaires)

## Intégration

Utilisé par Kiosk et ScanNOrder (menu, fiche produit, upsell, pricing/commande),
pas par le POS — voir `docs/KIOSK_DECISIONS.md` (2026-09-26).

### Exemple
```go
unavailable, err := availabilitiesService.GetUnavailableProductsAt(ctx, merchantID, time.Now())
if _, hidden := unavailable[productID]; !hidden {
    // Inclure le produit dans le menu
}
```

## Notes

- ✅ Architecture : Handler → Service → Repository
- ✅ Transactions atomiques (3 tables)
- ✅ Suppression logique (enabled = 0)
- ✅ IDs UUID (CHAR(36))
- ✅ Heures stockées en heure locale du merchant (évaluées dans `merchant.timezone`)
- ✅ JSON tags en snake_case
- ✅ Pas de logs manuels (middleware)

---

**Voir `docs/AVAILABILITIES_MODULE_GUIDE.md` pour la documentation complète.**
