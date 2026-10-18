# Livrable 4 — Analyse critique du code et des algorithmes

Chaque constat indique : la preuve (fichier et lignes), l'impact sur la marge ou
sur la capacité à la mesurer, la correction proposée et l'opportunité du
backlog associée ([L5](L5-backlog-opportunites.md)). Un choix de conception n'est
signalé que si son impact est démontré ou plausible avec un mécanisme précis.

**Gravité** : *Critique* = empêche de savoir si l'upsell rapporte ; *Élevée* =
perte de CA ou de marge probable, ou décisions faussées ; *Moyenne* = perte
localisée ; *Faible* = hygiène.

**Mise à jour du 2026-10-16** : constats confrontés aux données de prod
([L9](L9-resultats-baseline-prod.md)). C-03, C-05, C-08, C-09, C-12 et C-15
sont complétés par les chiffres mesurés ; C-22 et C-23 sont nouveaux.
L'objectif arbitré est le **CA** avec le moins de friction possible : là où le
texte parle de marge, lire « CA espéré » tant que les coûts ne sont pas saisis.

## 0. Ce qui est sain (à conserver)

- **Dégradation maîtrisée** : `recover`, replis Redis/catalogue/LLM, aucune
  erreur 5xx renvoyée aux clients, délai client de 2 s qui ne bloque jamais le
  paiement. C'est exactement ce qu'il faut sur un parcours de caisse.
- **Hygiène statistique des associations** : P(B|A) lissée vers la fréquence
  moyenne (β = 10), seuils de support et de lift, maximum et non somme sur le
  panier (D12). C'est mieux que la plupart des « achetés ensemble » maison.
- **Gestion des variantes** (D1, D13), **filtres par canal avant sélection**
  (D9), **déterminisme** (même panier, même liste, départage par id) :
  reproductible et testable.
- **Tests par étape** ([service_test.go](../../../internal/modules/upsell/service_test.go),
  [tasks/upsell_test.go](../../../internal/tasks/upsell_test.go), tests
  d'intégration Postgres) et documentation des décisions au fil de l'eau.
- **Briques de mesure déjà en place** : `is_upsell` écrit par les trois canaux,
  coût de revient figé à la ligne (migration 114), journal des requêtes avec
  corps, réponse et durée (90 jours).

## 1. Mesure et incrémentalité

### C-01 — Aucun groupe témoin, aucune randomisation · *Critique* · confiance élevée

- **Preuve** : tout panier éligible reçoit la même liste
  ([service.go:148-175](../../../internal/modules/upsell/service.go#L148-L175),
  tri déterministe [L810-815](../../../internal/modules/upsell/service.go#L810-L815)) ;
  aucun mécanisme d'allocation dans le dépôt.
- **Impact** : impossible de distinguer une vente **attribuée** (le produit
  suggéré est dans la commande) d'une vente **incrémentale** (elle n'aurait pas
  eu lieu sans la suggestion). Aucun changement de règle ne peut être évalué :
  c'est la raison pour laquelle le remplacement du lift et l'exclusion des
  lignes `is_upsell` avaient été reportés.
- **Correction** : décision d'exposition (témoin / exposé / variante) prise côté
  serveur, avant le cache, déterministe par session ; voir
  [L8 R2](L8-recommandations-techniques.md#r2--groupe-témoin-et-couche-de-politique).
  → U-03.

### C-02 — Une ligne par appel API, pas par exposition · *Élevée* · confiance élevée

- **Preuve** : une ligne est créée à chaque appel, y compris en cache
  ([service.go:151-174](../../../internal/modules/upsell/service.go#L151-L174)) ;
  le POS appelle à chaque changement de composition du panier
  ([upsell_controller.dart:49-71](../../../../wello_resto_flutter/lib/controllers/order/upsell_controller.dart#L49-L71)) ;
  SNO et la borne abandonnent l'étape après 2 s mais la ligne existe déjà ;
  SNO et la borne n'affichent que 6 articles au plus, filtrés encore après le
  moteur.
- **Impact** : le « taux de transformation » de l'onglet Analytics
  (acceptées / proposées, [analytics/upsell.go:427-448](../../../internal/modules/analytics/upsell.go#L427-L448))
  divise par des appels, pas par des sessions exposées. Il est biaisé vers le
  bas, surtout au POS (plusieurs lignes par commande), et non comparable entre
  canaux. Un pilotage fondé dessus conclurait à tort que l'upsell ne convertit
  pas.
- **Correction** : enregistrer ce qui est réellement renvoyé au client (filtres
  du canal et plafond appliqués dans le moteur, voir C-10 et C-21) et lier
  chaque exposition à la commande qui suit, acceptée ou non. → U-01, U-04.

### C-03 — Une acceptation vide est comptée comme acceptée · *Élevée* · confiance élevée

- **Preuve** : le Tracker écrit toujours un tableau, même vide
  ([tracker.go:130-137](../../../internal/modules/upsell/tracker.go#L130-L137)) ;
  l'Analytics compte `accepted_items IS NOT NULL`. Au POS, l'identifiant de
  suggestion acceptée n'est effacé qu'à la réinitialisation de la commande
  ([order_controller.dart:569-570](../../../../wello_resto_flutter/lib/controllers/order/order_controller.dart#L569-L570),
  [L1039-1040](../../../../wello_resto_flutter/lib/controllers/order/order_controller.dart#L1039-L1040)),
  pas quand l'article est retiré — contrairement à SNO et à la borne.
- **Scénario** : un serveur tape une suggestion, puis retire l'article ; la
  commande part avec l'identifiant ; `accepted_items = []`, comptée acceptée.
- **Impact** : taux d'acceptation POS surestimé, de manière inconnue.
- **Mesuré (B1)** : 3 lignes sur 90 jours (2 à la borne de 212, 1 chez
  l'établissement de test). Faible aujourd'hui parce que le suivi POS ne se
  déclenche jamais (C-05) ; le défaut grossira quand il sera réparé.
- **Correction** : compter `jsonb_array_length(accepted_items) > 0` ; au POS,
  effacer l'identifiant quand plus aucune ligne `isUpsell` ne reste. → U-01.

### C-04 — Le contexte de décision n'est pas conservé · *Élevée* · confiance élevée

- **Preuve** : la ligne ne garde que l'empreinte du panier, ni ses produits, ni
  le type de commande, ni la version des règles, ni la latence totale (seule
  celle du LLM, [service.go:574-583](../../../internal/modules/upsell/service.go#L574-L583)).
  Une exposition sans acceptation n'a pas d'`order_id`.
- **Impact** : impossible d'apprendre des refus (« ce panier a vu ces produits
  et n'en a pris aucun »), donc impossible d'estimer des taux d'acceptation par
  contexte, condition de tout classement par valeur espérée. Seul
  `api_request_logs` garde le corps des appels, 90 jours.
- **Correction** : colonnes `cart_product_ids`, `order_type`, `policy`,
  `engine_version`, `latency_total_ms` ; identifiant d'exposition transmis à la
  commande dans tous les cas. → U-04.

### C-05 — Attribution approximative et incomplète · *Moyenne* · confiance élevée

- **Preuve** : l'acceptation est l'intersection « produits suggérés ∩ produits
  de la commande » par `product_id`
  ([tracker.go:116-128](../../../internal/modules/upsell/tracker.go#L116-L128)) ;
  `revenue_impact` est en TTC, sans coût ([L139-144](../../../internal/modules/upsell/tracker.go#L139-L144)) ;
  `suggestedPrices` est calculé et jamais lu ([L100](../../../internal/modules/upsell/tracker.go#L100)) ;
  le suivi n'est appelé qu'à la création de commande
  ([order_life_cycle/service.go:1164-1188](../../../internal/modules/order_life_cycle/service.go#L1164-L1188)).
- **Impact** : au POS, un produit suggéré mais ajouté par le menu est compté
  comme accepté ; un dessert suggéré et ajouté plus tard à une commande de salle
  (`UpdateOrder`) n'est jamais compté. L'indicateur mélange TTC et marge.
- **Confirmé par les données (L9 §4)** : aucune acceptation POS enregistrée
  sur 90 jours, alors que 4 lignes POS `is_upsell` existent. Le POS envoie
  l'identifiant aussi lors des mises à jour de commande
  ([order_draft_manager.dart:475](../../../../wello_resto_flutter/lib/controllers/order/order_draft_manager.dart#L475)),
  que le serveur ignore.
- **Correction** : attribuer par ligne (`is_upsell`, désormais écrit par les
  trois canaux), en HT, avec coût figé quand il existe ; suivre aussi les
  ajouts en `UpdateOrder`. → U-08.

### C-06 — Le classement maximise la probabilité d'achat, pas l'effet de la suggestion · *Élevée* · confiance moyenne

- **Preuve** : le score des associations est P(B|A) lissée
  ([tasks/upsell.go:228-231](../../../internal/tasks/upsell.go#L228-L231)) ; les
  petits prix sont classés par ventes ([L386-394](../../../internal/tasks/upsell.go#L386-L394)).
- **Mécanisme** : les paires les mieux classées sont celles que les clients
  achètent le plus souvent **spontanément** (prod, 2026-09-28 : Escalope →
  Soda 59 %). Ce qui compte pour la marge est l'écart
  P(achat | exposé) − P(achat | non exposé).
  - Au **POS**, la barre est visible pendant la prise de commande : une
    suggestion acceptée peut remplacer un ajout que le serveur aurait fait de
    toute façon. Incrémentalité probablement plus faible.
  - Sur **SNO et la borne**, le popup arrive quand le panier est arrêté ; un
    client qui voulait une boisson l'a souvent déjà prise, et le produit
    proposé est absent du panier. Incrémentalité probablement plus forte, mais
    le classement par P(B|A) mesuré sur *toutes* les commandes ne dit rien de
    la propension *résiduelle* à ce moment-là.
- **Impact** : risque de surestimer la valeur de l'upsell (si l'on raisonne en
  ventes attribuées) et de mal classer les produits. Non quantifiable sans
  témoin.
- **Correction** : mesurer l'écart par canal et par origine (U-03), puis
  classer sur l'écart observé quand le volume le permet (U-18). Ne **pas**
  remplacer P(B|A) avant d'avoir le témoin.

### C-07 — Boucle auto-renforçante · *Moyenne* · confiance élevée

- **Preuve** : les lignes `is_upsell` entrent dans les associations
  ([tasks/upsell.go:88-98](../../../internal/tasks/upsell.go#L88-L98)), dans les
  ventes des petits prix ([L322-330](../../../internal/tasks/upsell.go#L322-L330))
  et dans `is_popular`.
- **Impact** : un produit suggéré se vend, donc reste suggéré ; l'effet croît
  avec le nombre d'établissements activés. Les statistiques de « propension »
  se mettent à mesurer la réponse à la suggestion. Déjà au « Reporté ».
- **Correction** : exclure ces lignes des statistiques de propension, les
  garder pour estimer l'acceptation (voir §6). → U-16.

## 2. Classement et rentabilité

### C-08 — Scores hétérogènes, priorité figée par étape · *Élevée* · confiance élevée

- **Preuve** : association = P(B|A) lissée, entre 0,10 et 1 ; petit prix =
  **0,5 constant** ([service.go:898](../../../internal/modules/upsell/service.go#L898)) ;
  LLM = score auto-déclaré. Les associations passent toujours devant
  ([L248-254](../../../internal/modules/upsell/service.go#L248-L254)). La barre
  POS surligne les scores ≥ 0,8 comme « association historique forte »
  ([upsell_suggestions_bar.dart:28-31](../../../../wello_resto_flutter/lib/ui/widgets/home/upsell/upsell_suggestions_bar.dart#L28-L31)),
  ce que seuls des scores LLM ou des associations très rares atteignent.
- **Scénario (prod 212, 2026-09-28)** : Pizza Poulet → Coca, P(B|A) ≈ 10 %,
  juste au seuil, passe toujours devant l'Orangina, en tête des petits prix et
  présente avec presque toutes les pizzas (Pizza fromage → Orangina : lift
  1,16, donc une boisson achetée « partout »). Personne ne sait laquelle des
  deux a la plus forte probabilité d'ajout : les deux scores ne sont pas sur la
  même échelle, et la priorité tranche à la place des données.
- **Impact** : des places de tête attribuées à des associations faibles ; le
  surlignage POS ne signifie pas ce qu'il prétend.
- **Mesuré (L9 §3, SNO et borne)** : le produit d'une association est ajouté
  2 fois sur 266 présentations (1 sur 217 en première place), un petit prix
  46 fois sur 3 482 (14 sur 464 en première place). Dans les listes qui
  commencent par une association, chez 212 et 226, 14 ajouts sur 15 portent
  sur un petit prix situé plus bas. Petits effectifs, mais aucune donnée ne
  justifie la priorité systématique des associations.
- **Correction** : ramener les petits prix sur la même échelle (part lissée des
  commandes contenant le produit, déjà calculable dans le cron) et classer tous
  les candidats sur une seule valeur. → U-09.

### C-09 — Ni le prix ni la marge n'entrent dans le classement ; à égalité, le moins cher gagne · *Élevée* · confiance élevée (constat), faible (gain)

- **Preuve** : aucune variable de prix dans le score des associations ;
  petits prix triés par ventes puis **prix croissant**
  ([tasks/upsell.go:386-394](../../../internal/tasks/upsell.go#L386-L394)) ;
  `cheapestVariants` retient la variante **la moins chère** d'un groupe
  ([service.go:733-765](../../../internal/modules/upsell/service.go#L733-L765)).
  Le coût figé n'existe que pour les produits à recette
  ([cost.go:55-84](../../../internal/modules/order_life_cycle/cost.go#L55-L84)) ;
  un produit revendu tel quel est `NO_RECIPE`.
- **Impact** : à acceptation égale, le moteur choisit la plus petite marge
  absolue. Le choix « plus petit ajout » (D13) est défendable pour
  l'acceptation, mais il n'a jamais été confronté à la marge, faute de mesure.
  Effet de bord utile d'un classement par valeur : les produits à 0 € (frais de
  livraison, sauces offertes, risque accepté en D12 sur le POS) tomberaient
  naturellement en dernier, sans règle sur les noms.
- **Mesuré (B4)** : aucune des 4 397 lignes des produits suggérés n'a de coût
  connu chez les établissements réels. Le mécanisme existe (coût d'achat des
  composants, recette), les données ne sont pas saisies.
- **Objectif arbitré : le CA.** La variable qui compte est alors le prix : le
  départage au moins cher et la variante la moins chère vont aussi contre
  l'objectif.
- **Correction** : valeur espérée = probabilité × prix HT (U-12 révisé) ; tester
  la variante « la plus vendue » contre « la moins chère » (sonde U-22) ; la
  marge (probabilité × marge HT) quand les recettes des produits suggérés
  seront saisies (U-11).

### C-10 — Disponibilité par mode de commande filtrée après le moteur · *Moyenne* · confiance élevée

- **Preuve** : `AvailableProduct` n'a pas les drapeaux sur place / emporter /
  livraison ([menu/repository.go:110-125](../../../internal/modules/menu/repository.go#L110-L125)) ;
  SNO les filtre après coup ([scannorder/service.go:1659](../../../internal/modules/scannorder/service.go#L1659)),
  la borne aussi ([kiosk/service.go:1657](../../../internal/modules/kiosk/service.go#L1657)).
  Le prix utilisé par le moteur est le prix de base, pas celui du mode.
- **Impact** : même classe de défaut que celui corrigé par D9 : chaque produit
  retiré est une place perdue, non remplacée. Le seuil « petit prix » est
  calculé sur un prix qui n'est pas forcément celui affiché.
- **Correction** : lire ces drapeaux et le prix du mode dans la liste des
  candidats, filtrer avant la sélection. → U-05.

### C-11 — Aucune diversité imposée · *Moyenne* · confiance moyenne

- **Preuve** : la règle de catégorie ne s'applique qu'aux associations ; les
  petits prix remplissent dans l'ordre des ventes. UPSELL_COMPLETION estime
  qu'à 212, sans règle, environ 4 suggestions sur 6 sont des boissons.
- **Impact** : un client prend rarement plusieurs boissons d'un coup ; 4
  places de boissons sur 6 coûtent des places de desserts ou d'accompagnements.
  Proposer une deuxième boisson à un panier qui en contient une est peu utile.
- **Correction** : au plus 2 articles par catégorie (la catégorie comme simple
  étiquette, sans interpréter les noms, compatible avec le rejet de P1),
  catégories déjà au panier en dernier. À tester. → U-10.

### C-12 — Pas de politique « ne rien proposer » · *Moyenne* · confiance moyenne

- **Preuve** : SNO et la borne ouvrent l'étape dès qu'un article reste
  ([useUpsell.ts:90-95](../../../../wello-resto-scannorder/src/hooks/useUpsell.ts#L90-L95),
  [cart_screen.dart:74](../../../../wello-kiosk/lib/presentation/screens/cart_screen.dart#L74)) ;
  la liste des petits prix n'est vide que si le catalogue n'a aucun produit
  sous le seuil.
- **Mesuré (B6)** : 818 listes non vides sur 820 appels SNO et borne : le
  popup s'ouvre à presque chaque paiement, à l'opposé de l'objectif « le moins
  de friction possible ».
- **Impact** : un écran supplémentaire avant presque chaque paiement, y compris
  quand la meilleure suggestion a une valeur espérée infime. Avec un gain
  mesuré de 0,10 € TTC payé par session, une hausse d'abandon d'environ
  **0,6 point** suffit à rendre l'étape négative en CA, et ce seuil est trop bas pour être
  mesuré au volume actuel ([L9 §6](L9-resultats-baseline-prod.md#6-ce-que-cela-change)).
- **Correction** : seuil de valeur espérée en dessous duquel l'étape est sautée
  (U-13), ou suggestions intégrées au panier sans étape bloquante (U-28), en
  test contre l'affichage systématique.

### C-13 — L'accroche est calculée mais affichée nulle part · *Faible* · confiance élevée

- **Preuve** : `title` est produit par les modèles de phrase ou le LLM
  ([prompts.go:27-32](../../../internal/modules/upsell/prompts.go#L27-L32)) ;
  ni la popup SNO, ni la borne, ni la barre POS ne l'affichent.
- **Impact** : le principal apport du LLM (une accroche) est perdu ; aucune
  raison n'est donnée au client (« souvent pris avec votre pizza »).
- **Correction** : tester une raison contextuelle, qui demande de garder le
  produit source de l'association. → U-14.

## 3. LLM (désactivé en prod)

### C-14 — Sélection des candidats par ordre alphabétique · *Faible* (tant que coupé) · confiance élevée

- **Preuve** : 50 candidats au plus, triés **par nom** puis tronqués
  ([service.go:928-938](../../../internal/modules/upsell/service.go#L928-L938)) ;
  le retry partage le délai de 1,5 s du premier essai
  ([L333](../../../internal/modules/upsell/service.go#L333),
  [L352-361](../../../internal/modules/upsell/service.go#L352-L361)) ; la
  température par défaut est 0,5 ([ai.go:42](../../../internal/config/ai.go#L42)).
- **Impact** : au-delà de 50 candidats, le LLM ne voit que les produits du
  début de l'alphabet. Le retry n'a presque jamais le temps d'aboutir. Si
  `AI_TASK_UPSELL_MODEL` pointe un jour vers un modèle qui refuse la
  température (Opus 5.5, Sonnet 5.5, voir CLAUDE.md), chaque appel échouera.
  Scores non calibrés.
- **Correction** : ne pas réactiver le LLM pour **choisir** les produits sans
  preuve de gain ; s'il revient, le limiter à la rédaction de l'accroche, avec
  présélection par score. → U-25.

## 4. Performance et robustesse

### C-15 — Latence non suivie, coût d'enrichissement linéaire · *Faible* (rétrogradé) · confiance élevée

> **Mesuré (B5, 30 jours)** : p50 265-312 ms, p95 690-835 ms, p99 1,27-1,57 s ;
> 12 appels sur 3 114 au-delà du délai client de 2 s (0,4 %). La latence n'est
> pas un problème : seule l'écriture de la latence totale (U-04) reste utile,
> le chargement des fiches en lot ne l'est pas.

- **Preuve** : une lecture de fiche produit complète par suggestion
  ([service.go:620-633](../../../internal/modules/upsell/service.go#L620-L633)),
  lecture du catalogue complet à chaque appel hors cache ; latence totale non
  enregistrée ; SNO et la borne abandonnent après 2 s.
- **Impact** : un appel lent fait sauter l'étape sans trace dans
  `upsell_suggestions` (la ligne existe, rien ne dit qu'elle n'a pas été vue).
  Non mesuré : la requête B5 donne p50/p95/p99 et la part d'appels au-delà de
  2 s depuis `api_request_logs`.
- **Correction** : enregistrer la latence totale ; charger les fiches en lot
  si B5 montre un p95 proche de 2 s. → U-04, U-07.

### C-16 — Calcul nocturne non supervisé · *Moyenne* · confiance élevée

- **Preuve** : durée de vie Redis 36 h ([tasks/upsell.go:27](../../../internal/tasks/upsell.go#L27)) ;
  aucune table d'exécution ; échecs par établissement seulement journalisés.
- **Impact** : deux nuits ratées et le moteur ne renvoie plus que des listes
  vides (`none`), silencieusement.
- **Correction** : alerte sur l'âge de `_meta.computed_at` et sur la part de
  `none` / `featured_fallback`. → U-07.

### C-17 — Données d'apprentissage mélangées · *Moyenne* · confiance moyenne

- **Preuve** : la requête des paniers prend toutes les commandes `CLOSED`, sans
  filtre de canal ni de `brand_status`
  ([tasks/upsell.go:88-98](../../../internal/tasks/upsell.go#L88-L98)), alors
  que l'Analytics exclut `DELETED`/`CANCELED`.
- **Impact** : les paniers Uber Eats et Deliveroo (autres prix, autre carte,
  aucun upsell affiché) pèsent dans les associations proposées en salle ; des
  commandes annulées peuvent être comptées (à vérifier sur la prod).
- **Correction** : restreindre aux canaux maison (`order_source`) et exclure
  les annulations ; à tester sur la couverture (requête de comparaison
  existante). → U-16.

## 5. Architecture

### C-18 — Responsabilités dispersées · *Moyenne* · confiance élevée

- **Preuve** : filtres répartis entre le moteur (canal, horaires) et les
  enveloppes SNO/borne (mode de commande, horaires à nouveau) ; classement
  écrit en ligne dans `generateUpsellSafe` ; seuils en constantes globales ;
  aucune notion de politique ou de version.
- **Impact** : ajouter un groupe témoin ou une variante de classement
  aujourd'hui revient à multiplier les `if` dans une fonction de 170 lignes.
  Ce qui est enregistré diffère de ce qui est affiché.
- **Correction** : découpage ciblé, sans réécriture : candidats → filtres (tous
  au même endroit) → scoreurs → re-classement (diversité, plafond) →
  politique (bras d'expérience) → journal. À faire au fil de U-03 et U-09.
  → U-23.

### C-19 — POS : plusieurs listes par commande, une seule transmise · *Faible* · confiance élevée

- **Preuve** : « dernière acceptée gagne »
  ([upsell_controller.dart:34-43](../../../../wello_resto_flutter/lib/controllers/order/upsell_controller.dart#L34-L43)) ;
  libellé « Proposé par IA » quel que soit l'origine
  ([upsell_suggestions_bar.dart:139](../../../../wello_resto_flutter/lib/ui/widgets/home/upsell/upsell_suggestions_bar.dart#L139)),
  alors que le LLM est coupé.
- **Impact** : exposition par commande non reconstructible au POS ; libellé
  inexact pour le personnel.
- **Correction** : garder le POS hors des premiers tests ; corriger le libellé
  (« Suggestion »). → U-01.

### C-20 — Liste identique pour un même panier · *Faible* · confiance élevée

- **Preuve** : cache de 30 min et tri déterministe.
- **Impact** : un habitué voit toujours les mêmes produits (fatigue possible,
  non mesurée) ; aucune exploration, donc rien à apprendre sur les produits
  jamais proposés.
- **Correction** : la décision de politique avant le cache (R2) permet plus
  tard une rotation contrôlée. → U-19.

### C-21 — Plafond serveur et plafond client divergent · *Faible* · confiance élevée

- **Preuve** : `upsell_max_items` jusqu'à 10 ; SNO et la borne coupent à 6.
- **Impact** : les articles 7 à 10 sont enregistrés comme proposés sans jamais
  être vus ; le réglage marchand est trompeur sur ces canaux.
- **Correction** : appliquer le plafond d'affichage côté serveur par canal, ou
  faire suivre le réglage par les clients. → U-06.

## 5 bis. Constats issus des données de prod (2026-10-16)

### C-22 — Une acceptation est enregistrée avant le paiement et jamais révisée · *Élevée* · confiance moyenne

- **Preuve** : le Tracker est déclenché à la création de la commande
  ([order_life_cycle/service.go:1164-1188](../../../internal/modules/order_life_cycle/service.go#L1164-L1188)),
  avant le paiement sur la borne et SNO ; rien ne le révise si la commande est
  ensuite refusée, annulée ou jamais payée.
- **Données (B8)** : 8 acceptations sur 45 (18 %) portent sur une commande
  jamais payée : 4 annulées par le personnel à la borne de 226 (semaine du
  lancement), 2 refusées automatiquement sur SNO (226), 2 annulées par le
  client à la borne (212, 231).
- **Impact** : l'acceptation et le CA attribués dans `upsell_suggestions`
  comptent des ventes qui n'ont pas eu lieu : 122 € suivis pour 1 000 sessions,
  101 € réellement payés.
- **Correction** : ne compter une acceptation que si la commande liée est
  clôturée et non annulée (jointure dans l'Analytics), ou s'appuyer sur les
  lignes `is_upsell` des commandes clôturées (U-08). → U-01.

### C-23 — Le POS appelle le moteur même quand la barre n'est pas affichée · *Élevée* (mesure) · confiance élevée

- **Preuve** : la barre n'existe que dans la mise en page tablette
  ([menu_list_view.dart:275-276](../../../../wello_resto_flutter/lib/ui/views/home/menu_list_view.dart#L275-L276)) ;
  l'appel part à chaque changement de composition du panier, quel que soit
  l'écran ([order_controller.dart:251-252](../../../../wello_resto_flutter/lib/controllers/order/order_controller.dart#L251-L252)).
- **Données (L9 §4)** : à 212, 1 175 appels pour environ 382 commandes POS
  (3,1 par commande) et une seule ligne `is_upsell` ; à 241, 370 appels et aucune
  ligne.
- **Impact** : les lignes POS ne disent pas ce que le personnel a vu ; le
  nombre de suggestions « proposées » est gonflé ; charge inutile sur l'API et
  la base pour les terminaux téléphone.
- **Correction** : n'appeler que si la barre est affichée (ou transmettre un
  indicateur d'affichage enregistré sur la ligne). → U-27.

## 6. Points « Reporté » de UPSELL_COMPLETION : avis

| Point reporté | Avis | Opportunité |
|---|---|---|
| Associations catégorie → catégorie | Utile surtout comme signal **négatif** : un menu contient déjà une boisson (prod 226 : 9,6 % de boisson avec un menu contre 27,9 % avec un tacos seul). À traiter comme a priori lissé pour les produits sans association, après le témoin | U-21 (P3) |
| Remplacer le lift par une probabilité lissée | Fait (D12) | — |
| Exclure les lignes `is_upsell` | **Oui** pour les statistiques de propension (associations, `is_popular`, ventes des petits prix) ; **non** pour estimer l'acceptation. À faire après U-03, pour que l'effet soit mesuré | U-16 |
| Suivi des acceptations | C'est le cœur de l'audit : exposition liée à la commande et groupe témoin | U-01, U-03, U-04 |
| *(b)* Varier les catégories | À tester : plafond de 2 par catégorie | U-10 |
| *(c)* Ne pas élargir aux produits chers | Compatible avec les recommandations ; la montée en gamme est un autre type de suggestion, à tester à part | U-22 |
| *(d)* Suppléments vendus comme produits | Laisser en l'état ; la piste « options populaires » est incluse dans la montée en gamme | U-22 |
| Frais de livraison à 0 € proposés sur le POS (risque accepté, D12) | L'exclusion par le restaurateur (demandée) règle le cas directement ; un classement par CA espéré les met aussi en dernier | U-26, U-12 |
