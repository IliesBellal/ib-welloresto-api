# PROMPT 20 — Diagnostic vente additionnelle (avant correctifs)

Date : 2026-09-05. Phase 1 uniquement (diagnostic), conformément à la
consigne du prompt : « le diagnostic compte davantage que les correctifs ».
Chiffres vérifiés en lecture seule contre `RENDER_STAGING_DATABASE_URL`
(`DB_DIALECT=postgres`, outils jetables `go run` supprimés après usage,
requêtes dans ce document reconstructibles à l'identique).

**Avertissement sur l'environnement** : je n'ai pas d'accès direct à la base
de production depuis cet environnement (seul `RENDER_STAGING_DATABASE_URL`
est disponible localement). Les chiffres ci-dessous viennent de staging.
Ils correspondent cependant presque exactement à ceux cités par le prompt
comme figures de production (287 vs 284 côté « établissement de test »,
26 vs 26 côté « reste des établissements », 1 acceptation dans les deux cas)
— l'écart (313 vs 284 au total aujourd'hui) s'explique par le temps écoulé
depuis la session qui a produit ces chiffres. Soit staging reflète fidèlement
la production sur ce périmètre, soit les deux bases contiennent des données
convergentes pour d'autres raisons — à confirmer par le lecteur avant de
considérer ces chiffres comme définitifs pour la prod réelle.

---

## 1.1 — Pourquoi si peu de suggestions ?

### Chiffres réels (staging, 2026-09-05)

```
upsell_suggestions total : 313 lignes
  merchant=2   (établissement de test) : 287
  merchant=212 (établissement réel)    : 26
  (28 autres établissements)           : 0 — ZÉRO ligne chacun
```

Répartition par `source` :

```
featured_fallback : 312
pattern            : 1
llm / cached_*     : 0
```

Répartition par `channel` : POS 196, KIOSK 83, SNO 34 — les trois canaux
appellent bien `GenerateUpsell` et créent des lignes (contredit toute
hypothèse d'un canal totalement muet côté génération).

### Cause n°1 (dominante) : `enable_upsell` n'est activé que sur 2
établissements sur 30

```sql
SELECT enable_upsell, COUNT(*) FROM merchant_parameters GROUP BY enable_upsell;
-- false: 28   true: 2   (merchant_id 2 et 212)
```

`generateUpsellSafe` ([internal/modules/upsell/service.go:126-128](../../internal/modules/upsell/service.go#L126))
retourne `SourceDisabled` **avant tout appel à `CreateSuggestion`** quand
`enable_upsell = false`. Aucune ligne `upsell_suggestions` n'est donc même
tentée pour ces 28 établissements — **peu importe leur volume de
commandes**. Preuve directe : les établissements 235 (617 commandes CLOSED/
90j), 226 (348), 234 (189) ont chacun **zéro** ligne `upsell_suggestions`,
alors que leur volume dépasse largement celui de 212 (26 lignes pour 2013
commandes). Ce n'est pas un bug — la fonctionnalité n'a simplement jamais
été activée en configuration pour l'écrasante majorité des établissements.
**C'est un arbitrage produit, pas un défaut technique.**

### Cause n°2 : même activé, le moteur "intelligent" ne se déclenche
quasiment jamais — tout retombe sur le fallback statique

Sur les 313 lignes générées (pour les 2 établissements activés), **312 sont
`featured_fallback`** (liste statique de produits "featured", dernier
recours) et **1 seule est `pattern`**. Aucune n'est `llm`/`cached_llm` :
soit les clés IA (`ANTHROPIC_API_KEY`/`OPENAI_API_KEY`) ne sont pas
configurées dans cet environnement et `aiRegistry.GetProviderForTask` échoue
systématiquement ([service.go:253-260](../../internal/modules/upsell/service.go#L253)),
soit le LLM échoue/timeout à chaque appel (`llmTimeout = 1500ms`) — les deux
mènent au même repli. À vérifier côté configuration de l'environnement
concerné (variables d'env absentes vs présentes mais invalides).

Le chemin `pattern` exige `bestLift >= 1.5` **ET**
`len(aggregated) >= maxItems` simultanément
([service.go:219](../../internal/modules/upsell/service.go#L219)) — une
condition composée, pas un simple seuil de lift. Le fait qu'elle ne soit
remplie qu'une seule fois sur 313 tentatives, **y compris pour
l'établissement 212 qui a 2013 commandes clôturées sur 90 jours**, confirme
que le seuil de co-occurrence (`upsellMinCoOccur = 5`,
[internal/tasks/upsell.go:16](../../internal/tasks/upsell.go#L16)) fragmente
trop la matrice de co-occurrence pour un catalogue de restaurant classique
(dizaines de produits, commandes réparties sur beaucoup de paires
possibles) : même avec un volume de commandes non négligeable, peu de paires
de produits atteignent 5 co-occurrences distinctes sur une fenêtre de 90
jours. **Constat produit, conforme à l'hypothèse du prompt** : le seuil
calibré semble structurellement difficile à atteindre à cette échelle.

Preuve indirecte que le cron `RecomputeUpsellPatterns` (3h chaque nuit)
**s'exécute bel et bien** au moins par intermittence : la présence même
d'**une** ligne `source=pattern` est impossible sans qu'un calcul de
patterns ait écrit au moins une entrée exploitable dans Redis au préalable
— sans quoi `aggregated` serait toujours vide et `bestLift` toujours à 0.
Ceci dit, comme demandé dans le prompt : **il n'existe aucune table ou
journal d'exécution des tâches planifiées** (grep exhaustif
`job_run|cron_log|task_execution|last_run_at` sur tout le dépôt : aucun
résultat) — seuls les logs Zap (`tm.logInfo("[CRON] RecomputeUpsellPatterns:
terminé", ...)`) tracent l'exécution, et ils ne sont pas persistés dans une
table interrogeable après coup. Je ne peux donc confirmer la régularité
(tourne-t-il vraiment toutes les nuits, ou a-t-il échoué silencieusement la
plupart du temps) qu'indirectement, via cette preuve statistique — **pas
une preuve d'exécution récurrente**.

### Cause n°3 : `CleanupOldUpsellSuggestions` n'est pas en cause

Purge mensuelle, seuil `upsellCleanupMonths = 8`
([internal/tasks/upsell.go:21](../../internal/tasks/upsell.go#L21)). La
ligne la plus ancienne en base date du 2026-05-05 (merchant 2), soit ~4 mois
— **rien n'a encore atteint le seuil de purge**. Cette tâche n'explique
aucune partie du déficit observé aujourd'hui, mais mérite d'être surveillée
: à 8 mois, le seuil est plus long que le rythme de production observé sur
la plupart des établissements réels, donc pas de risque de purger des
suggestions encore utiles au rythme actuel.

### Cause n°4 (partielle, non tranchée) : même activé, l'endpoint est
appelé sur une fraction infime des commandes réelles

Établissement 212 : 26 suggestions générées pour 2013 commandes clôturées
sur 90 jours, soit ~1,3 % des commandes. Ce ratio bas peut venir soit d'un
déclenchement conditionnel côté client (barre d'upsell affichée seulement
dans certains cas), soit d'un usage réel très occasionnel par le personnel.
**Non tranché dans ce diagnostic** — nécessiterait la lecture du
déclencheur d'appel côté POS Flutter (`upsell_suggestions_bar.dart` et son
appelant), hors du périmètre exploré ici en profondeur.

---

## 1.2 — Le signal d'acceptation fonctionne-t-il ?

**Oui, le code fonctionne** — mais il n'a été exercé qu'une seule fois dans
toute l'histoire de la base, et cette unique fois date d'avant le correctif
ScanNOrder (voir 1.3/2.1).

```
accepted_items non-vide : 1 ligne sur 313
order_id renseigné      : 1 ligne sur 313 (la même)
```

Détail de cette unique ligne :

```
suggestion_id = upsell-sugg-c2ea7ff6-...  merchant=2  channel=SNO
source=featured_fallback  created_at=2026-07-02 11:13:52
accepted_items=[{"product_id":"76","quantity":1,"unit_price":750}]
revenue_impact=7.5
→ order_id=32175, orderitem product=76, is_upsell=FALSE
  (order state=CLOSED, brand_status=CLOSED)
```

Cette acceptation date du **2026-07-02**, jour exact de l'audit
`docs/audits/audit_upsell_traceability.md` qui concluait « ScanNOrder :
`is_upsell` jamais transmis côté item ❌ ». C'est cohérent : à cette
date-là, ScanNOrder ne posait effectivement pas le flag — le produit
suggéré (76) a fini dans la commande (`Tracker.TrackAsync` l'a donc
correctement recoupé et comptabilisé comme "accepté"), mais sans que la
ligne `orderitems` ne porte `is_upsell=true`, faute d'émission côté client
à l'époque.

**Point important pour l'interprétation du taux 0,35 % cité par le
prompt** : `accepted_items` mesure « le produit suggéré est-il dans la
commande finale », **indépendamment de si le client a effectivement tapé
sur la suggestion** — c'est une corrélation passive par `product_id`, pas
une preuve d'interaction UI (documenté ainsi dans
`tracker.go`, section « Accumulate accepted items »). `orderitems.is_upsell`
mesure autre chose : « cette ligne vient précisément du tap sur la
suggestion ». Les deux signaux sont donc structurellement différents et ne
doivent pas être comparés comme s'ils mesuraient la même chose — le
rapprochement fait dans le prompt (287 proposées / 1 acceptée ↔ is_upsell
resté à zéro) est correct dans son constat mais la seconde partie
(is_upsell) ne peut **pas** servir à corroborer ou infirmer la première
(accepted_items), ce sont deux mesures indépendantes.

**Conclusion** : le code d'acceptance n'est pas cassé — il est simplement
**inexercé** depuis 2 mois, faute de suggestions générées en volume (cf.
1.1). Impossible de vérifier empiriquement si le correctif ScanNOrder
(2.1 ci-dessous, déjà en place) fonctionne réellement en conditions réelles
: aucune acceptation ne s'est produite depuis son déploiement pour le
tester.

---

## 1.3 — Quel build tourne en salle ?

**Aucune trace serveur exploitable aujourd'hui pour répondre avec
certitude**, mais deux pistes existantes et une nouvelle recommandation :

1. **`app_version` / `app_version_merchant`** (tables existantes) ne
   servent qu'à décider si une mise à jour est *disponible* — elles
   reçoivent le numéro de version du client
   (`AuthRepository.CheckAppVersion`,
   [internal/modules/auth/repository.go:827](../../internal/modules/auth/repository.go#L827))
   mais **ne le persistent jamais**. Aucune table ne conserve "quel
   établissement tourne sur quelle version" après coup — confirmé par
   lecture complète de la fonction : elle ne fait que des `SELECT`.
   Par ailleurs la table `app_version` elle-même semble à l'abandon : la
   dernière entrée réelle (hors lignes `version_code=0`/date zéro, bruit de
   seed) date du **2025-04-14** (`version_code=63`), alors que l'appli
   envoie déjà `version=100` aujourd'hui (voir point 2) — l'écart de 37
   versions suggère que ce mécanisme de contrôle de mise à jour n'est plus
   maintenu/utilisé activement pour forcer les mises à jour.

2. **Bonne nouvelle partielle** : `api_request_logs.payload` (jsonb, déjà
   en place, alimenté par `internal/middleware/request_logger`) capture
   déjà le corps de chaque requête, y compris `/app/version/check`. Exemple
   réel trouvé en base :
   ```
   payload = {"app": "WR_RECEPTION", "version": "100"}
   ```
   Le numéro de version rapporté par le client **est donc déjà capturé
   durablement**, quelque part — simplement jamais exploité pour cette
   question, et surtout : **`merchant_id` est vide sur ces lignes**
   (confirmé par requête), donc impossible de savoir *quel établissement*
   a envoyé cette version sans autre corrélation (ex. `user_id` +
   `api_request_logs` sur une route authentifiée proche dans le temps).

**Recommandation (le plus léger, comme demandé)** : plutôt que créer une
nouvelle table, faire renseigner `merchant_id` sur les lignes
`api_request_logs` générées par la route `CheckAppVersion` (le token est
déjà décodé à ce stade — `helpers.ExtractToken` puis résolution utilisateur
dans `AuthService.CheckAppVersion`) — extension mineure d'un mécanisme déjà
en place, pas un nouveau système. Cela suffirait à répondre à "quel
établissement tourne sur quelle version" par une requête SQL directe sur
`api_request_logs`, sans migration ni nouvelle table. **Je ne l'ai pas fait
— c'est un changement de comportement de logging, pas un simple
correctif, et il touche une route d'auth partagée : à valider avant
d'implémenter.**

**Sur la question initiale (build antérieur vs barre jamais utilisée)** :
le diagnostic 1.1 (seulement 2 établissements sur 30 avec `enable_upsell`,
et un taux d'appel de ~1,3 % des commandes même sur l'établissement actif)
rend la question largement **sans objet à l'échelle globale** — la plupart
des builds en salle n'ont simplement jamais l'occasion d'exercer ce chemin,
peu importe leur version. Pour les 2 établissements concernés, impossible
de trancher formellement entre "build antérieur au 25 juin" et "barre
utilisée mais jamais tapée" avec les données actuelles.

---

## État réel du code Phase 2 (vérifié, pas supposé)

Le prompt suppose un état du code (notamment "ScanNOrder n'envoie jamais
`is_upsell` — aucune occurrence dans le dépôt") qui **s'est révélé faux à
la vérification** :

### 2.1 — ScanNOrder : déjà câblé, aucun correctif nécessaire

Vérifié dans le HEAD committé actuel de `wello-resto-scannorder` (7 commits,
pas un dépôt à un seul commit malgré ce que suggérait `git log` sur un
fichier isolé) :
- `src/types/menu.ts:98` : `isUpsell?: boolean` sur `CartItem`.
- `src/components/cart/UpsellPopup.tsx:133` : `isUpsell: true` posé à
  l'ajout au panier depuis la suggestion.
- `src/components/catalogue/ProductModal.tsx:171` : propage `isUpsell` lors
  de la configuration produit.
- `src/lib/api/payload.ts:55` : `is_upsell: item.isUpsell ?? false` —
  transmis au backend, exactement comme demandé par le prompt (aligné sur
  le kiosk).

C'est exactement le même schéma que Kiosk et POS. **Ce travail a déjà été
fait entre le 2026-07-02 (date de l'audit qui le signalait manquant) et
aujourd'hui**, sans qu'aucune trace n'en existe dans `docs/decisions.md` ni
dans un audit de suivi — probablement une session de travail non
documentée. Aucune ligne de code n'a donc été touchée ici pour 2.1 : il n'y
avait rien à corriger.

*Nuance* : le dépôt `wello-resto-scannorder` a par ailleurs des
modifications non commitées actuellement en cours (`CheckoutFlow.tsx`,
`ProductCard.tsx`, allergènes, `delivery_travel_seconds`...) — travail en
cours visiblement sans rapport avec l'upsell, non touché.

### 2.2 — Commentaire périmé : corrigé

[internal/modules/stats/service.go:106](../../internal/modules/stats/service.go#L106)
mis à jour — l'ancien commentaire affirmait que l'app Flutter n'écrivait
pas encore `is_upsell` (« Sprint 2 »), ce qui est faux depuis juin pour le
POS et a été vérifié faux aussi pour Kiosk et SNO dans ce diagnostic. Le
nouveau commentaire pointe vers le présent document plutôt que de réexpliquer
la cause (variation dans le temps, dette de documentation).

### 2.3 — Seuil de co-occurrence : non modifié, remonté ci-dessous

Conformément à la consigne explicite du prompt, **je n'ai pas touché**
`upsellMinCoOccur`/`minLift`/`upsellMinConfidence`
([internal/tasks/upsell.go](../../internal/tasks/upsell.go),
[internal/modules/upsell/service.go](../../internal/modules/upsell/service.go)).
Voir arbitrages produit ci-dessous.

---

## Arbitrages produit à trancher (pas du code)

1. **`enable_upsell` désactivé sur 28/30 établissements** — est-ce
   intentionnel (rollout pilote limité à 2 établissements) ou un oubli
   d'activation généralisée ? C'est la cause n°1 du volume observé, de très
   loin. Avant toute activation à grande échelle, il faudrait d'abord
   trancher le point 2 ci-dessous (sinon activer 28 établissements de plus
   ne fera qu'ajouter des lignes `featured_fallback`, pas des suggestions
   pertinentes).

2. **Le moteur pattern/LLM ne produit quasiment jamais rien même quand
   activé** (312/313 = fallback statique). Si les clés IA sont absentes de
   cet environnement, c'est un point de configuration à corriger avant de
   juger le moteur LLM inopérant. Si elles sont présentes et que le LLM
   échoue quand même systématiquement, ça mérite une investigation dédiée
   (hors périmètre de ce diagnostic). Séparément : le seuil de
   co-occurrence (5 co-occurrences/90 jours) semble structurellement dur à
   atteindre pour un volume de restaurant indépendant classique — recalibrer
   ce seuil est un arbitrage produit (un seuil trop bas produirait des
   suggestions absurdes), pas un correctif que je me permets de faire seul.

3. **Traçabilité du build client** : aucun mécanisme actuel ne permet de
   savoir quel établissement tourne sur quelle version d'app. Le correctif
   le plus léger identifié (renseigner `merchant_id` sur les logs
   `CheckAppVersion`) n'a pas été implémenté — changement de comportement
   de logging sur une route d'auth partagée, à valider avant d'y toucher.
