# Data Migration

Ce dossier sert d'espace de travail local pour les exports MySQL bruts avant transformation vers Postgres.

Flux attendu:
1. Exporter une table depuis phpMyAdmin en CSV ou en SQL par table.
2. Deposer le fichier source dans ce dossier, sans jamais committer de donnees reelles.
3. Lancer le script de transformation sur la table cible.
4. Recuperer un CSV prete a charger dans Postgres.

Principes:
- Les colonnes booleennes sont converties de `1/0` vers `true/false`.
- Les colonnes `merchant_id` de la liste du rapport 13 restent en texte pour rester compatibles avec `varchar(64)`.
- Les identifiants d'identite sentinelles explicites, comme `tva_categories.tva_id = -1`, sont detectes et signales comme necessitant `OVERRIDING SYSTEM VALUE` au chargement.
- Tout le reste est conserve tel quel.

Exemples d'usage:

```bash
python data-migration/transform_mysql_csv.py inspect --table tva_categories
python data-migration/transform_mysql_csv.py transform --table available_languages --input data-migration/available_languages.csv --output data-migration/available_languages.pg.csv
```

Le script peut aussi servir d'outil d'audit local: il dit si une table necessite une transformation ou si un chargement direct suffit.

Regle pratique:
- Transformation requise si la table contient au moins une colonne booleenne, une colonne `merchant_id` du rapport 13, ou un identifiant d'identite sentinelle explicite a conserver.
- Chargement direct si aucune de ces conditions ne s'applique.
