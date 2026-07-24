# 53 — `audit_logs.id` trop étroite (`InsertLogWithChain`, écart transverse #7 bis)

Erreur observée en test Postgres : `value too long for type character varying(36)` sur
`InsertLogWithChain` ([repository.go:113](../../internal/modules/audit/repository.go#L113), log
de l'erreur remontée par l'`INSERT` exécuté juste au-dessus, lignes 92-110). Aucune donnée réelle
citée ci-dessous — uniquement le schéma et le code.

## 1. Colonne exacte concernée

Ce n'est **pas** un hash de chaînage : `previous_hash varchar(64)` et `hash varchar(64) NOT NULL`
sont déjà à la bonne largeur dans `04-schema-postgres-target.sql` (lignes 141-142) et correspondent
exactement à un SHA-256 hexadécimal (64 caractères) — cf. `sha256.Sum256` formaté en `%x` dans
`InsertLogWithChain` ([repository.go:89](../../internal/modules/audit/repository.go#L89)).

La colonne trop étroite est en réalité `audit_logs.id` (ligne 133 de
`04-schema-postgres-target.sql`) :

```sql
CREATE TABLE audit_logs (
    id varchar(36) NOT NULL,   -- <- trop étroite
    ...
```

Largeur MySQL source (`wello-resto-mysql-ddl.md:104`) : **`id` varchar(36) NOT NULL** — la même
largeur que la cible Postgres. La colonne n'a pas été mal traduite ; le schéma source lui-même est
déjà trop étroit pour la valeur réellement générée (voir §3).

## 2. Générateur Go et longueur réelle

`AuditService.LogChange` ([service.go:40](../../internal/modules/audit/service.go#L40)) est le
seul point de génération de cet `id` :

```go
ID: helpers.GeneratePrefixedID(helpers.AuditLogIDPrefix),
```

`helpers.AuditLogIDPrefix = "audit-log"` ([ids.go:12](../../internal/helpers/ids.go#L12)) et
`GeneratePrefixedID` ([ids.go:67-69](../../internal/helpers/ids.go#L67-L69)) produit
`prefix + "-" + uuid.New().String()` :

| Élément | Longueur |
|---|---|
| `"audit-log"` | 9 |
| séparateur `"-"` | 1 |
| `uuid.New().String()` (format `8-4-4-4-12`) | 36 |
| **Total** | **46 caractères** |

C'est déterministe et systématique (un seul call site, pas de variante selon le flux d'appel,
contrairement à `customer_loyalty_progress.id` dans le rapport 28) : **chaque** insertion via
`InsertLogWithChain` porte un `id` de 46 caractères, donc chaque insertion réelle échoue en
Postgres, jamais seulement certaines.

## 3. MySQL source : même largeur 36 — troncature silencieuse, pas une erreur de traduction

Le schéma MySQL source a exactement la même contrainte `varchar(36)` que la cible Postgres
traduite. Ce n'est donc pas un écart introduit par la traduction du schéma : c'était déjà un défaut
dans le schéma MySQL de production. C'est une nouvelle occurrence de l'écart transverse #7 déjà
identifié en Tier 3 (["Troncature varchar MySQL non-strict"](27-tier3-conversion-log.md), confirmé
et corrigé pour trois autres colonnes dans le [rapport 28](28-varchar-widening.md)) : en mode non
strict, MySQL tronque silencieusement une chaîne trop longue au lieu de rejeter l'`INSERT` ;
Postgres n'a pas d'équivalent et rejette avec l'erreur `22001` — ce qui est exactement ce qui a
révélé le problème ici.

Contrairement aux trois colonnes du rapport 28, aucune troncature `[:36]` n'a été ajoutée côté Go
pour `audit_logs.id` pendant le Tier 3 — le grep annoncé au Tier 4 ("longueur des IDs préfixés vs
largeur de colonne") n'a pas couvert le module `audit` : `29-tier4-conversion-log.md` ne mentionne
aucune ligne `audit`. Le test d'intégration Postgres du module
([postgres_integration_test.go](../../internal/modules/audit/postgres_integration_test.go)) ne
l'a pas non plus détecté, car il construit ses `id` de test à la main (`"itest-audit-1"`, 13
caractères) au lieu de passer par `AuditService.LogChange` / `helpers.GeneratePrefixedID` — il
n'exerce donc jamais le chemin de génération d'id réellement utilisé en production.

**Conclusion** : en MySQL non strict, chaque `id` stocké est déjà tronqué à 36 caractères
(`"audit-log-"` + les 26 premiers caractères de l'UUID) au lieu des 46 caractères prévus. Comme
`id` est la `PRIMARY KEY`, une collision entre deux troncatures identiques aurait provoqué une
erreur `1062` au moment de l'insertion (pas de corruption silencieuse possible) — même raisonnement
que pour `customer_loyalty_progress.id` au rapport 28.

## 4. Correction du schéma cible Postgres

[`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) — `audit_logs.id` :
`varchar(36)` → `varchar(64)`. 64 plutôt que 46 exact pour rester cohérent avec la convention déjà
adoptée au rapport 28 (`users.token`, `customer_loyalty_progress.id`,
`customer_loyalty_progress_order.progress_id`, tous portés à `varchar(64)`), qui laisse de la marge
sans changer de classe de stockage (préfixe de longueur 1 octet, valable jusqu'à 255).

```sql
CREATE TABLE audit_logs (
    id varchar(64) NOT NULL,
    user_id varchar(36),
    ...
```

## 5. Revalidation `pglast`

Même méthode que les rapports [13](13-merchant-id-schema-update.md) /
[18](18-order-id-schema-update.md) / [26](26-planning-day-comments-integration.md) /
[28](28-varchar-widening.md) :

```
python3 -c "
import pglast
with open('docs/migration-postgres/04-schema-postgres-target.sql', encoding='utf-8') as f:
    sql = f.read()
stmts = pglast.parse_sql(sql)
print('PARSE OK -', len(stmts), 'statements')
"
→ PARSE OK - 457 statements
```

Même compte qu'après les rapports 26/28 (aucune instruction ajoutée/retirée, seule une largeur de
colonne modifiée).

## 6. Migration MySQL réelle nécessaire

Oui — la contrainte existe aussi côté MySQL (§3), donc une migration réelle est nécessaire en plus
du schéma cible Postgres, sur le même modèle que le rapport 28 :

[`migrations/067_widen_audit_logs_id.up.sql`](../../migrations/067_widen_audit_logs_id.up.sql) /
[`.down.sql`](../../migrations/067_widen_audit_logs_id.down.sql) — prochain numéro libre après 066.

```sql
ALTER TABLE audit_logs
  MODIFY id varchar(64) NOT NULL;
```

`id` est la `PRIMARY KEY` de `audit_logs` ; élargir un `varchar(36)` vers `varchar(64)` reste dans
la même classe de stockage (préfixe de longueur 1 octet), donc MySQL exécute ce `MODIFY` en place
sans reconstruire l'index — même raisonnement que `customer_loyalty_progress.id` au rapport 28.

> ⚠️ **Changement de schéma MySQL réel en production**, distinct de la bascule Postgres à venir. À
> appliquer et tester séparément (staging d'abord). Non exécutée par ce chantier.

### Requête de vérification à faire tourner en prod avant d'appliquer la migration

Aucun accès aux données de production n'était disponible ici (comme au rapport 28). `id` étant la
`PRIMARY KEY`, une collision de troncature aurait déjà provoqué un rejet `1062` au moment des
faits — pas de corruption silencieuse possible — mais la requête ci-dessous vaut d'être lancée par
prudence avant d'élargir la colonne :

```sql
-- Distribution des longueurs stockées — une concentration à exactement 36
-- caractères indique des id déjà tronqués par le mode non strict
SELECT LENGTH(id) AS len, COUNT(*) AS n FROM audit_logs GROUP BY len ORDER BY len DESC;

-- Vérification de principe (PRIMARY KEY, doublons structurellement impossibles)
SELECT id, COUNT(*) AS n FROM audit_logs GROUP BY id HAVING n > 1;
```

Si la première requête montre une concentration à 36 caractères, élargir la colonne n'allonge pas
rétroactivement les `id` déjà tronqués en base (les lignes historiques resteront à 36 caractères) ;
seules les nouvelles insertions bénéficieront de la largeur complète. Contrairement à
`users.token` (rapport 28), `id` n'est référencé par aucune clé étrangère et sert uniquement de
`PRIMARY KEY` interne à la table — aucune valeur applicative (session, lien externe) ne dépend de
sa forme tronquée, donc aucune régénération n'est nécessaire après migration.

## 7. Code Go

Aucune modification nécessaire : contrairement aux trois colonnes du rapport 28, aucune troncature
Go n'avait été ajoutée à imiter/retirer pour `audit_logs.id`. `GeneratePrefixedID` reste inchangé.

## 8. Vérification

- `go build ./...` : OK (aucun fichier `.go` modifié par ce chantier).
- Validation `pglast` de `04-schema-postgres-target.sql` : OK, 457 statements, aucune erreur.
- `postgres_integration_test.go` du module `audit` n'a pas été modifié par ce chantier ; noté au
  §3 comme angle mort (id de test à la main au lieu de `helpers.GeneratePrefixedID`) — hors
  périmètre ici, mais vaut un ticket de suivi pour éviter qu'un futur écart de largeur sur cette
  table passe à nouveau inaperçu.

## 9. Risque connexe identifié, hors périmètre de ce chantier

`audit_logs.resource_id` (`varchar(36)`) et `audit_logs.merchant_id` (`varchar(64)`) reçoivent des
identifiants provenant de nombreux modules appelants de `AuditService.LogChange` (`users`,
`planning/*`, `haccp`, `orders`, `order_life_cycle` — 8 fichiers au total). Certains de ces modules
utilisent des préfixes `GeneratePrefixedID` plus longs que `"audit-log"` (ex. `"plan-week-tpl-shift"`
19 caractères, `"haccp-trace-photo"` 18 caractères), ce qui produirait des `resource_id` de 55+
caractères — potentiellement aussi trop larges pour `varchar(36)`. Ce chantier n'a corrigé que la
colonne `id` responsable de l'erreur rapportée ; `resource_id` n'a pas été audité colonne par
colonne pour chaque appelant et mériterait le même traitement que le rapport 28 dans un chantier
dédié.
