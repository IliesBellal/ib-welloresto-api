# 49 — Premier chargement complet (147 fichiers) sur le Postgres de staging Render

Date: 2026-07-23
Branche: migration/postgres

## Objectif

Rejouer le protocole de répétition générale (rapports [36](36-full-data-load-rehearsal.md),
[38](38-full-data-load-rehearsal-v2.md), [39](39-full-data-load-rehearsal-v3.md),
[42](42-full-data-load-rehearsal-v4.md), [43](43-qrcodes-sequence-fix-and-full-rehearsal-v5.md)),
mais pour la première fois contre l'instance Postgres de **staging Render** plutôt que le Docker
de dev jetable — schéma déjà confirmé conforme au rapport [48](48-render-staging-schema-check.md)
(181 tables, vides). **Aucune donnée réelle n'est citée dans ce rapport.** Rien n'a été commité.

Écart de méthode assumé par rapport aux rapports précédents : `psql` n'est pas installé sur ce
poste. Le chargement a été fait via un script Go jetable (`pgx/v5`, déjà une dépendance du repo),
une connexion neuve par fichier, chaque fichier envoyé en une seule fois via le protocole simple
de Postgres (équivalent de `psql -f fichier.sql` sans paramètres liés) — comportement visé
identique à `-v ON_ERROR_STOP=1` : à la première erreur, plus aucune instruction du fichier
n'est exécutée, et la fermeture immédiate de la connexion laisse Postgres annuler tout ce que le
`BEGIN;`/`COMMIT;` du fichier n'a pas atteint. Fichiers traités strictement dans l'ordre numérique
(`001_...` → `147_...`), un par un.

## 1. Régénération des 147 fichiers

`generate-all-sql` relancé sur le dump réel, dans un dossier temporaire hors dépôt :

- **147/147 tables générées, 0 échec** (`failed_tables: {}`).
- **Total : 472 774 lignes** — identique au rapport 43, dump source inchangé.
- `dropped_null_key_rows_by_table: {"orderitems": 2}`,
  `dropped_source_columns_by_table: {"customer": ["is_migrated"], "orders": ["isDelivery"]}` —
  inchangés par rapport au rapport 43. **Aucune règle n'a changé** depuis la dernière répétition
  générale réussie sur Docker.

## 2. Chargement séquentiel contre Render staging

**Résultat : arrêt au fichier 2/147.**

```
OK     1/147  001_allergens.sql
FAILED 2/147  002_api_request_logs.sql
ERROR: unexpected EOF
```

Conformément à la consigne, l'exécution s'est arrêtée **immédiatement** à cette erreur — aucun
fichier suivant n'a été tenté, aucune correction ni nouvelle tentative n'a été faite.

### Nature de l'erreur — distincte des blocages précédents

**Ce n'est pas une erreur Postgres** : aucun code d'erreur SQL, aucun message de contrainte, de
syntaxe ou de type (contrairement aux blocages des rapports 38, 39, 42, qui étaient tous des
rejets côté serveur avec message explicite). `unexpected EOF` est une erreur **côté client**,
signifiant que la connexion TCP s'est terminée avant la fin de lecture de la réponse attendue —
c'est-à-dire un problème de transport réseau, pas un problème de contenu des données ni du
générateur.

**Élément structurel notable** : `002_api_request_logs.sql` fait **48 618 258 octets** (515 496
lignes du fichier, 206 352 lignes de données attendues), envoyé en un seul message. C'est le
**2ᵉ plus gros fichier sur les 147** (le plus gros étant `005_audit_logs.sql`, 117 301 233 octets,
jamais atteint). Par comparaison, `001_allergens.sql` (chargé avec succès) fait 1 068 octets.
**Hypothèse, non confirmée** : une limite de taille de message ou un délai d'inactivité côté
réseau/proxy Render, plutôt qu'un défaut du fichier généré lui-même — cohérent avec une erreur
purement côté transport et avec le fait que ce fichier avait déjà été chargé sans problème contre
le Docker de dev local (rapport 43) avec un contenu strictement identique. **Aucune tentative
de vérification supplémentaire (retry, découpage en lots plus petits, etc.) n'a été faite** —
hors périmètre de cette session, arbitrage humain requis avant toute nouvelle tentative.

### Vérification de l'état après l'arrêt

Comptage en lecture seule sur les deux tables concernées :

| Table | Lignes | Attendu | Constat |
|---|---|---|---|
| `allergens` | 14 | 14 | ✅ conforme — fichier 1/147 intégralement commité |
| `api_request_logs` | 0 | 206 352 | ✅ vide — transaction annulée proprement, aucune insertion partielle |

Même mécanisme de rollback propre que celui observé au rapport 39 (§3) : le fichier en échec
contient son propre `BEGIN;`/`COMMIT;` ; l'erreur avant le `COMMIT;` suivie de la fermeture de la
connexion laisse la table cible dans l'état où elle était avant la tentative.

## 3. Vérifications non exécutées (bloquées par l'arrêt du chargement)

Conformément à la consigne, les étapes suivantes **n'ont pas été exécutées** :

- Chargement des 145 fichiers restants (`003_...` à `147_...`).
- Comptage de lignes complet sur les 147 tables vs comptage attendu.
- Les 6 requêtes applicatives Go (`GetOrder`, `GetCashRegisterReport`, `GetUserByToken`,
  `ComputePOSStatus`, suite `planning/schedule`, `CreateOrder`) : non tentées — aucune des tables
  de premier plan qu'elles requièrent (`orders`, `users`, etc.) n'est chargée à ce stade.
- Vérification de la resynchronisation des séquences identity (`orders`, `qrcodes`) via
  insertion applicative réelle : non tentée, pour la même raison.

## 4. Tests d'intégration automatisés

Conformément à la consigne, **`TestGetCashRegisterReport_Postgres` et aucun autre test marqué
`postgres_integration` n'ont été exécutés contre le staging Render** — seuls des scripts Go ad hoc
en lecture/écriture directe ont été utilisés pour cette vérification, jamais la suite de tests
automatisée réservée au Docker de dev.

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés (contenant de vraies données), le rapport JSON de génération et
le journal de chargement ont été supprimés du dossier temporaire local. Aucun fichier de sortie
contenant de vraies données n'a été conservé sur disque. Rien n'a été commité ; aucun fichier du
dépôt autre que ce rapport n'a été modifié.

**État actuel de la base de staging Render, laissé tel quel (aucune remise à zéro faite, non
demandée)** : 147 tables présentes (rapport 48), dont **1 partiellement — en réalité totalement —
chargée** (`allergens`, 14/14 lignes, conforme aux données cible), les **146 autres toujours
vides**. Ce n'est pas de la donnée de pollution/test : `allergens` contient exactement les lignes
cible attendues, simplement le chargement des 146 tables suivantes reste à faire.

## 6. Synthèse

| Étape | Résultat |
|---|---|
| Régénération des 147 fichiers | OK — 147/147, 0 échec, 472 774 lignes, identique au rapport 43 |
| Schéma cible sur Render staging | Déjà confirmé conforme avant cette session (rapport 48) |
| Chargement séquentiel | **Arrêté au fichier 2/147** (`api_request_logs`) — contre 147/147 sur Docker de dev (rapport 43) |
| Nature du blocage | Erreur réseau côté client (`unexpected EOF`), **pas** une erreur Postgres — première occurrence de cette classe d'erreur dans la série de répétitions |
| Hypothèse (non confirmée) | Limite de taille de message ou timeout réseau/proxy sur un fichier de 48,6 Mo envoyé en un seul message — à arbitrer, aucune correction tentée |
| Table chargée avant l'arrêt | `allergens` : 14/14 lignes, conforme |
| Rollback du fichier en échec | Confirmé propre — `api_request_logs` reste à 0 ligne |
| Comptages 147 tables / requêtes applicatives / séquences identity | Non exécutés — bloqués par l'arrêt, conformément à la consigne |
| Tests `postgres_integration` | Non exécutés contre Render staging, conformément à la consigne |
| Fichiers `.sql` générés | Supprimés en fin de session |
| Fichiers commités | Aucun |

**Point bloquant pour la suite (arbitrage humain requis, aucune tentative supplémentaire faite
dans cette session)** : décider comment traiter les fichiers volumineux (`api_request_logs`,
`audit_logs`, `orders`, `orderitems`, `products`, …) contre le réseau Render — par exemple
découper ces fichiers en lots plus petits, augmenter un éventuel délai côté client, ou confirmer
d'abord auprès de Render qu'aucune limite de taille de message n'est en cause côté leur
infrastructure — avant de retenter un chargement complet. La base de staging reste, dans l'état
actuel, avec uniquement `allergens` chargée.
