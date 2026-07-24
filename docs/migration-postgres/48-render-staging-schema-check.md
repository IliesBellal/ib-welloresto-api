# 48 — Vérification du schéma Postgres sur le staging Render

Objectif : confirmer que le schéma cible est bien chargé sur l'instance Postgres de staging
Render, en lecture seule, avant toute reprise de données réelle. Aucune donnée réelle n'est citée
dans ce rapport, et aucune information de connexion (hôte, port, identifiants) n'y figure.

## Méthode

Connexion en session `default_transaction_read_only = on` via un script Go jetable (`pgx/v5`,
déjà une dépendance du repo). Aucune écriture effectuée. Aucun chargement de données à cette
étape.

## 1. Nombre de tables

```sql
SELECT count(*) FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
```

→ **181 tables**, conforme au compte attendu du Docker Postgres de dev.

## 2. Comptage de lignes sur quelques tables clés

| Table | Lignes |
|---|---|
| `orders` | 0 |
| `customer` | 0 |
| `customers` | table absente (le schéma utilise `customer` au singulier, pas de table `customers`) |
| `users` | 0 |

Toutes les tables vérifiées sont vides — cohérent avec un schéma chargé sans reprise de données.

## Conclusion

Schéma cible confirmé sur le staging Render : 181 tables créées, aucune donnée chargée. Prêt pour
une éventuelle étape de reprise de données ultérieure (hors périmètre de cette vérification).
