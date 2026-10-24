# Ordre d'affichage des options d'un attribut configurable

Migration `192_configurable_attribute_options_display_order`. Codé le 2026-10-22.

## Problème

Le back-office permet de glisser-déposer les options d'un groupe (« Sauces », « Cuisson »…), mais l'ordre n'était persisté nulle part :

- `UpdateAttribute` désactive toutes les options puis ré-écrit chacune (`UPDATE` si elle a un `id`, `INSERT` sinon), sans aucune colonne d'ordre ;
- toutes les lectures de `configurable_attribute_options` se faisaient sans `ORDER BY` : ordre physique de Postgres, non garanti.

La caisse, la borne et ScanNOrder affichaient donc un ordre arbitraire, et un réordonnancement dans le back-office était perdu.

## Décisions

| # | Décision |
|---|---|
| D1 | Colonne `configurable_attribute_options.display_order integer NOT NULL DEFAULT 0` : rang de l'option **dans son attribut** (0, 1, 2…). |
| D2 | **L'ordre est la position dans le tableau `options` reçu.** Aucun champ d'ordre n'est lu dans le payload (le champ `order` que le back-office pose au glisser-déposer est ignoré). Ainsi les anciens clients, qui renvoient les options dans l'ordre reçu, ne cassent rien. |
| D3 | Reprise des données : ordre **alphabétique** (`LOWER(title)`, `id` en départage) par attribut, options désactivées comprises. Suit la collation de la base. |
| D4 | Ensuite : **ordre de création**. Une option ajoutée dans le back-office arrive en fin de tableau, donc en dernier. Les options créées seules par les webhooks Uber Eats / Deliveroo prennent `MAX(display_order) + 1` dans leur groupe. |
| D5 | Toutes les lectures trient `(display_order, id)` : à égalité, l'id identity donne l'ordre de création. |
| D6 | Le serveur renvoie les options **déjà triées** ; les clients les affichent dans l'ordre reçu. `display_order` est aussi exposé dans le JSON (`AttributeOption`, `ConfigurableOption`). |

## Code touché

- `internal/modules/menu/repository.go` : `GetAttributes`, `GetAttribute`, les trois chargements du menu (menu disponible, menu complet, un produit) lisent et trient ; `insertAttributeOptionsTx` et `UpdateAttribute` (branches `UPDATE` et `INSERT`) écrivent `display_order = position`. L'import de carte passe par `insertAttributeTx`, donc par le même chemin.
- `internal/modules/orders/orders_fetcher_builder.go` : les options d'une ligne de commande (réédition en caisse) sont triées en Go, le `WHERE` étant composé dynamiquement.
- `internal/webhook/deliveroo_orders/repository.go`, `internal/webhook/ubereats/repository/attribute_mapping_repo.go` : placement en fin de groupe.
- `internal/models/menu_models.go`, `internal/modules/menu/models.go` : champ `DisplayOrder`.
- Test : `internal/modules/menu/attribute_option_order_postgres_integration_test.go` (tag `postgres_integration`, à lancer après la migration).

## Clients

Aucun ne retrie les options ; tous les affichent dans l'ordre reçu. Rien à changer côté logique :

- **Caisse (Flutter)** : `OptionResponse` → `OptionDto` → `attribute.options` rendu tel quel.
- **Borne** : `ModifierGroup.options`, construit par `kiosk.Service` depuis le menu du `menuService`.
- **ScanNOrder** : `normalizeOption` puis `group.options.map`, menu issu du même `menuService`.
- **Back-office** : le glisser-déposer existait déjà et envoie le tableau dans le nouvel ordre ; seul le type `AttributeOption` documente `display_order`.

## Déploiement

1. **Appliquer la migration 192 AVANT le code** : les lectures du menu sélectionnent `display_order`, le menu ne se charge plus sans la colonne.
2. La migration marque `merchant_parameters.last_menu_update = now()` pour les établissements ayant des attributs : sans cela, la caisse répondrait « no_update_required » et garderait l'ancien ordre.
3. Les menus de la borne et de ScanNOrder sont cachés dans Redis (TTL en filet) : ils prennent le nouvel ordre à l'expiration ou à la prochaine modification du menu de l'établissement.
4. Lancer le test d'intégration : `go test -tags postgres_integration ./internal/modules/menu/ -run DisplayOrder`.

## Limites connues

- Les options des lignes de commande **déjà passées** (tickets, historique) ne sont pas ordonnées par ce rang : `order_item_configuration` ne stocke que les options choisies.
- Le tri alphabétique de reprise dépend de la collation de la base (accents et majuscules).
- Rollback (`192…down.sql`) : l'ordre saisi est perdu ; redéployer l'ancien code d'abord.
