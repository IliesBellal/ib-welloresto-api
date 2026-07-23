# 32 - Real Export Format Check (structurel uniquement, aucune donnee)

Date: 2026-07-20
Branche: migration/postgres

## Contexte

Un export complet de production a ete depose dans `data-migration/migration_welloresto_data.sql`.
Ce document evalue si cet export est directement exploitable par l'outillage decrit dans
[31-data-copy-tooling.md](31-data-copy-tooling.md), sans jamais citer de valeur reelle du fichier.
Toutes les verifications ci-dessous ont ete faites en ne remontant que des elements structurels:
noms de tables, noms de colonnes, types declares, nombres de lignes.

Le fichier reste couvert par `data-migration/.gitignore` (`*` en deny-all, seuls `.gitignore`,
`README.md` et `transform_mysql_csv.py` sont autorises au commit) : aucune donnee reelle ni
derive contenant des donnees reelles n'a ete ni ne sera commite.

## 1. Format confirme

Le fichier est un dump `phpMyAdmin` (`-- phpMyAdmin SQL Dump`, version 5.2.2, serveur
`11.8.8-MariaDB-log`), pas un export CSV. Structure standard:

- `CREATE TABLE` par table (180 occurrences), avec types MySQL natifs (`tinyint(1)`, `varchar`,
  `int(11)`, `longtext`, `datetime`, etc.) et commentaires de colonnes.
- `INSERT INTO \`table\` (\`col1\`, \`col2\`, ...) VALUES` suivi d'une ligne par ligne de donnees,
  chaque ligne etant un tuple `(...)` autonome se terminant par `,` ou `;`.
- Une table peut etre couverte par plusieurs blocs `INSERT INTO` successifs (export par lots) —
  observe par exemple sur les plus grosses tables (payments, orders, orderitems, api_request_logs).

Verifications faites directement sur le fichier (600k+ lignes, ~250 Mo):

- 180 `CREATE TABLE`, 166 tables avec au moins une ligne de donnees, 14 tables presentes en
  schema mais vides dans cet export.
- 591 607 lignes de donnees au total (une ligne de fichier = un tuple de valeurs).
- Sur l'integralite des 591 607 lignes de donnees: parentheses toujours equilibrees hors chaines,
  aucune chaine non terminee, aucune ligne de donnees qui ne se termine pas par `),` ou `);`.
  Consequence pratique: chaque ligne du fichier correspond a exactly une ligne de table, sans
  saut de ligne brut a l'interieur d'un champ (les retours a la ligne dans les donnees sont deja
  echappes par l'export), ce qui rend un parsing en streaming (ligne par ligne) fiable.
- Fichier valide en UTF-8 de bout en bout (aucune erreur de decodage stricte sur l'ensemble du
  fichier).
- Correspondance quasi totale entre les tables du dump et celles du schema cible
  ([04-schema-postgres-target.sql](04-schema-postgres-target.sql)): 165 tables communes avec
  donnees, 1 table du dump absente du schema cible (`user_status_view`, une vue MySQL exportee
  comme table, non pertinente pour la migration), 2 tables du schema cible absentes du dump
  (`api_calls`, `checkout_orderitems` — aucun `CREATE TABLE` correspondant dans l'export, donc
  aucune donnee a en tirer).

## 2. Decision: adapter le script plutot que repartir en CSV

Recommandation: **adapter `transform_mysql_csv.py` pour lire ce dump SQL directement**, ne pas
redemander un export CSV table par table.

Raisons:

- Le dump couvre deja les 166 tables peuplees en un seul fichier, avec la liste de colonnes
  explicite sur chaque bloc `INSERT INTO`. Redemander un export CSV reviendrait a repeter
  manuellement l'operation 166 fois depuis phpMyAdmin, pour un gain nul: la meme source MySQL,
  la meme session, donc les memes eventuels problemes de donnees.
- Le format est verifie mecaniquement analysable en streaming (voir section 1): pas besoin de
  charger 250 Mo en memoire, pas de ligne de donnees corrompue ou multi-ligne a gerer.
- Les types MySQL natifs presents dans les `CREATE TABLE` du dump (ex. `tinyint(1)` pour les
  booleens) recoupent et confirment les regles deja codees dans le script (booleens deduits du
  schema Postgres cible), sans les remplacer.

## 3. Travail effectue

`data-migration/transform_mysql_csv.py` a ete etendu (memes regles de transformation
qu'auparavant: booleens `1/0` -> `true/false`, colonnes `merchant_id` du rapport 13 conservees en
texte, identifiants sentinelles signales pour `OVERRIDING SYSTEM VALUE`) avec:

- Un parseur de tuples SQL conscient des guillemets et des echappements MySQL (pas un simple
  split sur les virgules), qui distingue une valeur `NULL` d'une chaine vide.
- `iter_dump_rows(dump_path)`: lecture en streaming du dump, associant chaque ligne de donnees a
  sa table et a la liste de colonnes issue de l'en-tete `INSERT INTO` correspondant.
- `inspect-dump --dump <fichier>`: rapport JSON structurel uniquement (tables, nombre de lignes,
  colonnes, besoin de transformation) — sans jamais lire de valeur de champ dans la sortie.
- `export-table-from-dump --dump <fichier> --table <table> --output <csv>`: extrait et transforme
  une seule table directement depuis le dump.
- `export-all-from-dump --dump <fichier> --output-dir <dossier>`: un seul passage streaming sur
  tout le fichier, un CSV Postgres-ready ecrit par table (evite de relire 250 Mo une fois par
  table).

## 4. Validation

Executee localement sur le fichier reel, en verifiant uniquement des compteurs et la structure
(jamais de contenu):

- `inspect-dump` recense 166 tables avec donnees et 591 607 lignes au total — identique au
  comptage manuel independant fait ligne par ligne sur le fichier brut.
- `export-all-from-dump` a traite les 166 tables en un seul passage (~38s), produit 166 fichiers
  CSV, et les compteurs de lignes par table obtenus par l'outil correspondent exactement (0 ecart
  sur les 166 tables) au comptage manuel independant.
- Aucune table "non mappee" (presente dans le dump sans correspondance dans le schema cible) sur
  les tables avec donnees.
- Un seul cas d'identite sentinelle detecte (`tva_categories`, 1 occurrence), coherent avec le cas
  deja documente dans [31-data-copy-tooling.md](31-data-copy-tooling.md).
- Les CSV et le rapport JSON generes pendant cette validation contenaient des donnees reelles: ils
  ont ete produits dans un repertoire temporaire hors du repo puis supprimes immediatement apres
  verification. Rien n'a ete commite.

## 5. Points de vigilance (a garder pour le chargement reel)

- 2 lignes de donnees (sur 591 607), dans les tables `audit_logs` et `customer`, contiennent un
  caractere de remplacement Unicode (`U+FFFD`) deja present dans le fichier source: un
  arrondi/perte d'encodage survenu en amont de cet export (pas du a notre parsing, un export CSV
  depuis la meme base presenterait la meme anomalie). A signaler comme donnee source degradee sur
  ces 2 lignes, independamment du choix SQL/CSV.
- Comme dans le pipeline CSV existant, une valeur `NULL` et une chaine vide `''` sont toutes deux
  ecrites comme cellule CSV vide en sortie. C'est une limite deja presente dans la convention CSV
  du projet, pas une regression introduite ici — a garder en tete si une colonne texte peut
  legitimement contenir `''` distinct de `NULL`.
- `products.img` (image encodee en base64) produit des lignes source jusqu'a ~930 Ko: sans impact
  sur le parseur (lecture ligne par ligne), mais a savoir si un outil de visualisation/diff est
  utilise plus tard sur le dump brut.
- 14 tables du schema sont vides dans cet export (aucune ligne `INSERT`) — normal si elles ne sont
  pas encore utilisees en production, mais a confirmer avant de considerer la migration de donnees
  complete.

## Conclusion

Le fichier depose est un dump SQL phpMyAdmin exploitable directement, format confirme et valide
structurellement. Pas besoin de repartir sur un export CSV: le script existant a ete adapte pour
consommer ce format en conservant exactement les memes regles de transformation deja validees.
