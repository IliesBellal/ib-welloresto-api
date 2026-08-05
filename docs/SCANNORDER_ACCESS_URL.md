# ScanNOrder — `access_url` sur GET /integrations/scannorder

Ajout du champ `access_url` à l'objet `integration` renvoyé par
`GET /integrations/scannorder`, pour que le back-office puisse afficher /
copier le lien public de la boutique ScanNOrder du marchand.

## 1. Contrat d'API

```jsonc
// GET /integrations/scannorder  ->  200
{
  "integration": {
    "platform": "scannorder",
    "active": true,
    // ... champs existants inchangés ...
    "access_url": "https://scannorder.welloresto.fr/restaurant/le-bistrot"
  }
}
```

- **Forme** : `{SCANNORDER_BASE_URL}/restaurant/{slug}`.
- **Type** : `string | null`. Jamais absent du JSON (pas de `omitempty`), pour
  que le client puisse toujours lire la clé.
- **`null` dans deux cas** : marchand sans QR code principal, ou
  `SCANNORDER_BASE_URL` non renseignée dans l'environnement.
- `PATCH /integrations/scannorder` renvoie le même objet `integration` (il
  relit via `Service.GetScanNOrder`) : le champ y apparaît donc aussi, sans
  travail supplémentaire.
- Le early-return « intégration inactive » du handler renvoie une struct nue —
  `access_url` y vaut `null`. Voulu : pas de lien tant que ScanNOrder n'est pas
  activé.

## 2. Implémentation

| Couche | Fichier | Changement |
|---|---|---|
| Config | `internal/config/scannorder.go` | **aucun** — `SCANNORDER_BASE_URL` était déjà chargée dans `cfg.ScanNOrder.SNORedirectBaseURL` |
| Modèle | `internal/modules/integrations/models.go` | `AccessURL *string \`json:"access_url"\`` + champ non exporté `slug string` |
| Repo | `internal/modules/integrations/repository.go` | sous-requête scalaire `slug` dans `GetScanNOrderIntegration` |
| Service | `internal/modules/integrations/service.go` | param `scannorderBaseURL` + `buildScanNOrderAccessURL()` |
| Câblage | `cmd/api/routes.go` | `cfg.ScanNOrder.SNORedirectBaseURL` passée à `integrationsModule.NewService` |

### Origine du slug

`qrcodes.code` du **QR principal** du marchand, c'est-à-dire la ligne
`qrcodes` sans `location_id` (pas une table) ni `user_id` (pas un serveur) et
non supprimée :

```sql
SELECT code FROM qrcodes
WHERE merchant_id = ?
  AND location_id IS NULL
  AND user_id     IS NULL
  AND deleted     = false
LIMIT 1
```

C'est la même définition que `kiosk.Repository.getMerchantSlug`, et c'est bien
la valeur que `scannorder.Repository` résout côté public (`WHERE qr.code = ?`).

## 3. Décisions, prises au fil de l'eau

1. **Réutiliser `SCANNORDER_BASE_URL` plutôt qu'ajouter une variable.**
   Constatée déjà chargée dans `config/scannorder.go` et déjà utilisée pour le
   même type d'URL par `scannorder.Service.GetBrand`. Une 2ᵉ variable aurait
   créé deux sources de vérité pour le même host.

2. **Forme de l'URL : `/restaurant/{slug}` — décision révisée.**
   La demande initiale était `BASE_URL/{slug}`. Signalé que les deux autres
   constructeurs d'URL ScanNOrder du repo insèrent un segment
   (`scannorder/service.go` → `/restaurant/{slug}`, `kiosk/service.go` →
   `/restaurants/{slug}/order/{id}`). Arbitrage retenu : `/restaurant/{slug}`,
   aligné sur le redirect de marque. L'incohérence singulier/pluriel entre les
   deux constructeurs existants n'a **pas** été traitée ici (hors périmètre).

3. **Sous-requête scalaire plutôt qu'un 2ᵉ appel repo ou un `LEFT JOIN`.**
   Le pool MySQL est plafonné à 1 connexion ouverte (contrainte Hostinger, cf.
   `internal/database/mysql.go`) : un aller-retour supplémentaire par requête
   coûte cher. Un `LEFT JOIN qrcodes` aurait pu multiplier les lignes (un
   marchand a N QR codes) ; la sous-requête `LIMIT 1` garde une ligne unique
   sans dépendre du `LIMIT 1` final.
   ⚠️ le paramètre `merchant_id` s'insère en **3ᵉ** position dans
   `QueryRowContext` (revenue, orders_count, **slug**, where).

4. **Le slug transite par un champ non exporté, pas par un `json:"-"`.**
   Repo et service sont dans le même package : `slug string` est invisible de
   `encoding/json` par construction, aucune balise à maintenir, et le slug brut
   ne fuite pas dans la réponse (non demandé par le client).

5. **La construction de l'URL vit dans le service, pas dans le repo.**
   Le repository ne connaît pas la config ; il renvoie une donnée de base, le
   service applique la règle métier. Découpe conforme au pattern
   Handler → Service → Repository du projet.

6. **`null` plutôt que chaîne vide quand le slug manque** (validé par le
   demandeur). Cohérent avec les autres champs optionnels de la struct
   (`logo_url`, `header_title`, …) déjà en `*string`.

7. **Slash final de la base URL absorbé** (`strings.TrimRight`), pour qu'une
   variable d'env renseignée `https://host/` ne produise pas `//restaurant/`.

## 4. Exploitation

- **Variable d'environnement** : `SCANNORDER_BASE_URL`, sans slash final
  (ex. `https://scannorder.welloresto.fr`). Déjà requise par le module
  scannorder — si elle est correcte en prod, il n'y a rien à déployer en plus.
- **Aucune migration SQL** : `qrcodes` et `scannorder_settings` existent déjà.
- **Diagnostic `access_url: null`** alors que le marchand est actif :
  ```sql
  SELECT code, location_id, user_id, deleted FROM qrcodes WHERE merchant_id = '<id>';
  ```
  Si toutes les lignes ont un `location_id` ou un `user_id`, le marchand n'a pas
  de QR principal — c'est une donnée à créer, pas un bug de l'API.

## 5. Tests — statut d'exécution honnête

| Test | Portée | Statut |
|---|---|---|
| `TestBuildScanNOrderAccessURL` (`service_test.go`, nouveau) | base+slug, slash final, base vide, slug vide, slug blanc | ✅ **exécuté, 5/5 PASS** |
| `TestIntegrationsRepository_Postgres` (étendu) | slug absent → `""` ; QR serveur et QR supprimé ignorés ; QR principal retenu ; `access_url` bout en bout via `NewService` | ⚠️ **non exécuté** — `POSTGRES_URL` non défini sur le poste ; le test compile (`go vet -tags postgres_integration` OK) mais a été *skippé* |
| `go build ./...` | tout le repo | ✅ OK |

Le volet Postgres reste donc à faire tourner dans un environnement disposant de
`POSTGRES_URL`.
