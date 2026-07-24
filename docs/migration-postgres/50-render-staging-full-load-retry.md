# 50 — Nouvelle tentative de chargement sur Render staging (test de reproductibilité) : erreur identique

Date: 2026-07-23
Branche: migration/postgres

## Objectif

**Nouvelle tentative demandée explicitement par l'utilisateur**, uniquement pour tester si
l'erreur réseau du rapport [49](49-render-staging-full-load.md) était transitoire ou
reproductible — pas une correction, ni une nouvelle règle de génération. Même protocole,
même dump, même fichiers régénérés à l'identique. **Aucune donnée réelle n'est citée dans ce
rapport.** Rien n'a été commité.

Cette tentative constitue l'arbitrage humain demandé au rapport 49 (« on veut un arbitrage
humain avant toute nouvelle tentative ») — ce n'est pas un retry automatique décidé seul.

## Écart par rapport au rapport 49 : reprise à partir du fichier 002, pas depuis 001

`allergens` (fichier `001`) était déjà chargée avec succès et conforme (14/14 lignes, rapport 49
§2) sur cette même base de staging Render — rejouer `001_allergens.sql` aurait provoqué un
conflit de clé sur des lignes déjà présentes, ce qui n'aurait pas été un test valide de
reproductibilité de l'erreur réseau observée. Vérifié avant reprise :
`allergens = 14`, `api_request_logs = 0` — état strictement identique à celui laissé à la fin du
rapport 49. Le chargement a donc repris à partir du fichier **002/147**, dans les mêmes
conditions (connexion neuve par fichier, protocole simple, arrêt immédiat au premier échec).

## 1. Régénération des 147 fichiers

Identique au rapport 49 : **147/147 tables générées, 0 échec, 472 774 lignes** — dump source et
règles de génération inchangés.

## 2. Résultat : l'erreur se reproduit à l'identique

```
SKIPPING 1 already-loaded files (verified separately, not re-run)
FAILED 2/147  002_api_request_logs.sql
ERROR: unexpected EOF
```

**Même fichier (`002_api_request_logs.sql`), même erreur exacte (`unexpected EOF`), même absence
de code d'erreur Postgres** que le rapport 49. Aucune correction, aucun changement de méthode
n'a été appliqué entre les deux tentatives — seule la connexion réseau et la charge de Render au
moment de chaque essai diffèrent. La reproduction à l'identique, à quelques minutes d'intervalle,
rend l'hypothèse d'un simple aléa ponctuel moins probable, et renforce plutôt celle d'une
contrainte structurelle du chemin réseau/proxy Render sur les messages de grande taille (le
fichier fautif reste le 2ᵉ plus gros des 147, 48 618 258 octets — voir rapport 49 pour le détail).

### Vérification de l'état après ce second arrêt

| Table | Lignes | Attendu | Constat |
|---|---|---|---|
| `allergens` | 14 | 14 | ✅ inchangé, toujours conforme |
| `api_request_logs` | 0 | 206 352 | ✅ toujours vide — rollback à nouveau propre |

Aucune donnée supplémentaire n'a été ajoutée ni corrompue par cette tentative.

## 3. Vérifications non exécutées

Mêmes raisons qu'au rapport 49 — bloquées par l'arrêt au fichier 2/147 : comptage complet des
147 tables, les 6 requêtes applicatives Go, resynchronisation des séquences identity. **Non
exécutées**, conformément à la consigne d'arrêt immédiat.

## 4. Tests d'intégration automatisés

Toujours **aucun test marqué `postgres_integration` exécuté contre le staging Render** — inchangé
par rapport au rapport 49.

## 5. Nettoyage

Les 147 fichiers `.sql` régénérés pour cette tentative, le rapport JSON de génération et le
journal de chargement ont été supprimés. Aucun fichier contenant de vraies données n'a été
conservé. Rien n'a été commité.

**État actuel de la base de staging Render, inchangé depuis le rapport 49** : `allergens`
chargée (14/14), les 146 autres tables toujours vides.

## 6. Synthèse

| Étape | Résultat |
|---|---|
| Nature de cette session | Nouvelle tentative demandée explicitement par l'utilisateur, pour tester la reproductibilité de l'erreur du rapport 49 |
| Régénération des 147 fichiers | OK — identique au rapport 49 |
| Point de reprise | Fichier 002/147 (001 déjà chargé et conforme, non rejoué) |
| Résultat | **Erreur reproduite à l'identique** — même fichier, même message (`unexpected EOF`), toujours pas d'erreur Postgres |
| État de la base après | Inchangé par rapport au rapport 49 — `allergens` conforme, `api_request_logs` vide, rollback propre |
| Comptages / requêtes applicatives / séquences | Non exécutés — toujours bloqués par l'arrêt |
| Tests `postgres_integration` | Toujours non exécutés contre Render staging |
| Fichiers commités | Aucun |

**Conclusion** : la reproduction à l'identique élimine l'hypothèse d'un incident réseau isolé et
pointe davantage vers une contrainte systématique côté chemin réseau/proxy Render pour ce
fichier (ou plus largement pour les messages de grande taille). Le point bloquant du rapport 49
reste entier et n'a pas été traité dans cette session (aucune correction tentée, conformément à
la consigne) : décider comment traiter les fichiers volumineux avant toute nouvelle tentative
(découpage en lots plus petits, vérification d'une limite connue côté Render, etc.).
