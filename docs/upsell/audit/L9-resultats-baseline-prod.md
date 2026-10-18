# Livrable 9 — Résultats de prod et réponses aux cinq questions (2026-10-16)

Analyse des sorties de [requetes-baseline-prod.sql](requetes-baseline-prod.sql)
(B0 à B7, exécutées sur la prod par Ilies le 2026-10-16) et des réponses aux
cinq questions du [README](README.md). Ce livrable prime sur les hypothèses des
livrables 1 à 8 ; ceux-ci ont été mis à jour en conséquence.

**Fenêtre du moteur actuel** : du 2026-09-28 (première semaine où la source
`low_price` apparaît dans B1, donc D10 à D13 en prod) au 2026-10-16, soit
19 jours. L'établissement de test 2 est exclu des calculs.

**B8** (état des commandes portant une acceptation) a été exécutée ensuite le
même jour ; ses résultats corrigent les chiffres de CA au §2 et au §5.

**Limite** : la sortie de B7 est coupée à 200 lignes (limite d'export) ; il
manque le POS de 231 après le 2026-09-07 et les établissements 234, 235, 236 et
241. Sans effet sur les conclusions SNO et borne.

## 0. Réponses et conséquences

| Question | Réponse | Conséquence |
|---|---|---|
| 1. Volume | Requêtes B0 et B6 | ~280 sessions upsell par semaine sur SNO et borne, tous établissements confondus (§1) : seuls de gros effets sont mesurables (§6) |
| 2. Coûts | Les restaurateurs peuvent saisir le coût d'achat des composants | Le mécanisme existe, pas besoin de nouvelle colonne. Mais **aucune** ligne des produits suggérés n'a de coût connu chez les établissements réels (B4, §5) : marge non mesurable aujourd'hui |
| 3. Objectif | **Le CA**, avec le moins de friction possible | Indicateur principal : CA incrémental par session éligible ; classement par CA espéré (probabilité × prix) ; la friction devient un critère de premier rang |
| 4. État déployé | Tout est en prod et utilisé ; la fréquence dépend du personnel | Fenêtre de référence fixée au 2026-09-28. Les données montrent un usage quasi nul de la barre POS et des appels POS sans affichage (§4) |
| 5. Contrôle marchand | Possibilité de retirer un article des suggestions, rien d'autre | Nouvelle opportunité U-26 ; pas de signal de charge cuisine (U-20 abandonné) |

## 1. Périmètre et volumes (B0, B6, B7)

`enable_upsell` est maintenant activé sur les 8 établissements actifs (contre 2
au 2026-09-27). Mais seuls ceux qui ont SNO, une borne ou la barre POS
(`pos_upsell_enabled`) exposent réellement des suggestions :

| Établissement | Commandes 90 j | SNO | Borne | POS | Exposition réelle |
|---|---|---|---|---|---|
| 212 | 2 798 | 42 | 138 | 1 802 | SNO, borne, barre POS (+ 816 marketplaces, sans upsell) |
| 236 | 1 422 | 0 | 0 | 1 422 | **aucune** (barre POS coupée) |
| 226 | 773 | 326 | 447 | 0 | SNO, borne |
| 235 | 652 | 0 | 0 | 652 | **aucune** |
| 231 | 263 | 9 | 151 | 103 | SNO, borne (barre POS coupée) |
| 234 | 116 | 0 | 0 | 116 | **aucune** |
| 241 | 111 | 0 | 0 | 111 | barre POS |

Rythme actuel, semaines complètes du 2026-09-28 et du 2026-10-05 : **environ
280 appels upsell et 215 commandes clôturées par semaine** sur SNO et borne,
tous établissements confondus. Les bornes de 212 et 231 n'existent que depuis
fin septembre. Le POS pèse bien plus (environ 4 200 commandes sur 90 jours), mais
sa barre n'est presque pas utilisée (§4).

## 2. Ce que produit le moteur actuel sur SNO et borne (B1, B3)

Une ligne `upsell_suggestions` SNO ou borne ≈ une session de paiement (aux
retours en arrière près).

| Établissement, canal | Sessions | Sessions avec ajout | Taux | CA TTC suivi | CA pour 1 000 sessions |
|---|---|---|---|---|---|
| 212 borne | 144 | 7 | 4,9 % | 11,77 € | 82 € |
| 212 SNO | 24 | 1 | 4,2 % | 1,90 € | 79 € |
| 226 borne | 182 | 8 (4 sur commande clôturée) | 4,4 % | 11,00 € | 60 € |
| 226 SNO | 150 | 8 | 5,3 % | 18,50 € | 123 € |
| 231 borne | 177 | 21 | 11,9 % | 40,00 € | 226 € |
| 231 SNO | 6 | 0 | — | 0 € | — |
| **Total** | **683** | **45** | **6,6 %** | **83,17 €** | **122 €** |

- Une session avec ajout rapporte en moyenne **1,85 € TTC** (48 articles au
  total, environ 1,73 € l'article) : des boissons et petits prix, comme prévu.
- Le CA attribué représente **0,5 % à 1,8 %** du CA SNO + borne selon
  l'établissement (212 borne 1,2 %, 226 environ 0,5 à 0,6 %, 231 borne 1,8 %).
- **Corrigé par B8** (état des commandes) : sur les 45 sessions avec ajout,
  **37 aboutissent à une commande payée** (69,27 €) et 8 non (13,90 €) :
  4 annulées par le personnel à la borne de 226, toutes la semaine du lancement
  (probablement des essais, à confirmer avec l'établissement) ; 2 refusées
  automatiquement sur SNO chez 226 (paiement ou acceptation non aboutis) ;
  2 annulées par le client à la borne (212, 231).
- **Référence retenue** : **5,4 %** des sessions avec un ajout payé, **101 € TTC
  pour 1 000 sessions**, 1,87 € par session avec ajout payé ; au rythme actuel,
  environ **1 500 € TTC par an** tous établissements confondus. C'est un CA
  *attribué*, pas *incrémental*.
- Repère, non causal : à la borne de 212, la semaine du 2026-09-21 (ancien
  moteur, produits `is_popular`) donnait 0 ajout sur 75 sessions, contre 7 sur
  144 depuis le 2026-09-28. Cohérent avec D10 et D11, mais la semaine et la
  nouveauté de la borne se confondent avec le changement de moteur.

## 3. Où se font les ajouts (B2, B1)

### Par position (SNO et borne, hors test)

| Position | 1 | 2 | 3 | 4 | 5 | 6 |
|---|---|---|---|---|---|---|
| Articles présentés | 681 | 649 | 642 | 592 | 592 | 592 |
| Articles ajoutés | 15 | 11 | 9 | 4 | 3 | 6 |
| Taux | 2,2 % | 1,7 % | 1,4 % | 0,7 % | 0,5 % | 1,0 % |

Les trois premières places font **73 %** des ajouts ; les places 4 à 6 en font
encore 27 %. Réduire à 3 articles n'est donc pas gratuit pour le CA.

### Par origine

- **Associations** : 2 ajouts sur 266 présentations (0,75 %). En première
  place : 1 sur 217.
- **Petits prix** : 46 ajouts sur 3 482 (1,32 %). En première place : 14 sur 464.

### Les paniers « à association » convertissent mieux… par les petits prix

| Établissement, canal | Sessions dont la liste commence par une association | dont avec ajout | Autres sessions | dont avec ajout |
|---|---|---|---|---|
| 212 borne | 42 | 4 (9,5 %) | 102 | 3 (2,9 %) |
| 212 SNO | 7 | 1 | 15 | 0 |
| 226 borne | 85 | 6 (7,1 %) | 97 | 2 (2,1 %) |
| 226 SNO | 60 | 4 (6,7 %) | 90 | 4 (4,4 %) |
| 231 borne | 23 | 2 (8,7 %) | 154 | 19 (12,3 %) |

Chez 212 et 226 : 15 ajouts sur 194 sessions à association (7,7 %) contre 9 sur
304 (3,0 %), p ≈ 0,02 (test exploratoire, non corrigé, petits effectifs). Mais
sur ces 15 ajouts, **14 portent sur un petit prix situé plus bas dans la
liste**, un seul sur le produit de l'association. 231 montre l'inverse.

Lecture : une association repère un panier plus enclin à ajouter quelque chose
(probablement un plat sans boisson), mais le produit de l'association, placé
en tête par construction, n'est presque jamais pris. La place la plus vue est
mal employée. Rien dans ces données ne justifie de garder les associations
systématiquement en tête ; c'est l'objet de U-09 (classement unique), désormais
appuyé par des chiffres.

## 4. POS (B1, B3, B7 et code)

- **212** : 1 175 appels pour environ 382 commandes POS sur la fenêtre (3,1 appels
  par commande), **1 seule** ligne `is_upsell`. **241** : 370 appels, aucune
  ligne.
- **Aucune acceptation POS n'est enregistrée** dans `upsell_suggestions` sur 90
  jours, alors que 4 lignes POS `is_upsell` existent (212 et test).
- **Code — appels sans affichage** : la barre n'est rendue que dans la mise en
  page tablette ([menu_list_view.dart:275-276](../../../../wello_resto_flutter/lib/ui/views/home/menu_list_view.dart#L275-L276),
  dans `_buildTabletLayout`), mais l'appel part à chaque changement de
  composition du panier, quel que soit l'écran
  ([order_controller.dart:251-252](../../../../wello_resto_flutter/lib/controllers/order/order_controller.dart#L251-L252)).
  Sur téléphone, chaque appel crée une ligne jamais affichée. → C-23.
- **Code — suivi perdu** : le POS envoie `upsell_suggestion_id` à la création
  **et** à la mise à jour de commande
  ([order_draft_manager.dart:475](../../../../wello_resto_flutter/lib/controllers/order/order_draft_manager.dart#L475),
  [L499](../../../../wello_resto_flutter/lib/controllers/order/order_draft_manager.dart#L499)),
  le serveur ne suit qu'à la création. Une suggestion tapée sur une commande
  déjà créée (brouillon ou table ouverte) n'est jamais suivie : C-05 confirmé
  par les données.
- Conclusion : on ignore combien de suggestions POS ont été réellement vues ;
  l'usage par le personnel est proche de zéro. Le POS est pourtant le seul
  canal **sans friction pour le client**.

## 5. Mesure, coûts, latence (B3, B4, B5, B6)

| Constat | Chiffre | Effet sur l'audit |
|---|---|---|
| Coût connu des produits suggérés (établissements réels) | **0** ligne sur 4 397 ; à 212, 1 309 lignes `INCOMPLETE_RECIPE` (recettes commencées mais incomplètes), ailleurs `NO_RECIPE` | marge non mesurable ; U-11 devient une saisie de données, en P3 |
| Acceptations sans commande payée (B8) | 8 sur 45 (18 %) : 4 annulées par le personnel (226 borne, semaine du lancement), 2 refusées automatiquement (226 SNO), 2 annulées par le client (212 et 231 borne) | nouveau constat C-22 ; le CA attribué passe de 122 € à 101 € pour 1 000 sessions |
| Acceptations vides | 3 lignes (212 borne 2, test 1) | C-03 réel mais faible ; il serait plus fort si le suivi POS fonctionnait |
| Listes non vides sur SNO et borne | **818 sur 820** (99,8 %) | C-12 confirmé : le popup s'ouvre à presque chaque paiement |
| Appels pour une commande, mêmes jours | borne 1,14 ; SNO 1,79 | sur SNO, beaucoup de retours au panier ou d'abandons après le popup ; non attribuable sans témoin |
| Latence serveur (30 j) | p50 265-312 ms, p95 690-835 ms, p99 1,27-1,57 s ; 12 appels sur 3 114 au-delà de 2 s (0,4 %) | C-15 rétrogradé : la latence n'est pas un problème |
| Panier moyen avant/après (B7) | 226 borne : 15,73 € → 16,00 € ; 226 SNO : 23,67 € → 23,53 € | aucun effet visible, comme attendu : 0,10 € par session se perd dans une variation hebdomadaire de ±2 €. Un avant/après ne mesurera jamais l'upsell |

## 6. Ce que cela change

### Objectif et indicateur

Indicateur principal : **CA incrémental par session éligible** (TTC suivi, HT
pour la comptabilité). La marge reste dans les définitions ([L7](L7-plan-mesure-profit.md))
pour le jour où les coûts seront saisis. Point de vigilance, une seule fois :
CA et profit ne vont pas toujours dans le même sens ; la règle *(c)* (pas
d'élargissement aux produits chers) borne ce risque.

### Ce qui est mesurable à ~280 sessions par semaine

| Question | Effectif nécessaire | Durée |
|---|---|---|
| Hausse d'ajout payé de +50 % (5,4 % → 8,1 %), 50/50 | ≈ 2 700 sessions | ≈ 10 semaines (12 si un témoin de 20 % tourne en parallèle) |
| Hausse d'ajout payé de +30 % (5,4 % → 7,0 %), 50/50 | ≈ 7 100 | ≈ 25 semaines |
| Abandon dégradé de 3 points, témoin 20 % | ≈ 2 900 | ≈ 10 semaines |
| Abandon dégradé de 0,6 point (seuil où le popup détruit du CA), 50/50 | ≈ 44 000 | ≈ 3 ans |

Seuil de rentabilité de l'étape : gain de 0,101 € TTC par session ÷ panier
moyen SNO + borne de 15,8 € TTC sur la fenêtre (526 commandes clôturées,
8 303 €) ≈ **0,6 point d'abandon**. Au volume
actuel, **on ne pourra pas prouver que le popup rapporte plus qu'il ne coûte en
abandons**. Conséquences :

1. la faible friction doit être **construite**, pas démontrée : afficher moins
   souvent (U-13) ou sans étape bloquante (U-28) ;
2. le groupe témoin (R2) reste utile comme **filet** contre une forte
   dégradation et comme mesure de long terme, commune à tous les
   établissements ; il n'est plus le passage obligé de chaque changement ;
3. seuls les changements à effet attendu important valent un test.

### Enjeux économiques à volume actuel

- CA attribué payé : ≈ 1 500 € TTC par an, tous établissements.
- **+1 point d'ajout payé** (10 sessions sur 1 000 × 1,87 €) ≈ +18,7 € pour
  1 000 sessions ≈ **+270 € par an**.
- **+0,1 point d'abandon** (1 commande perdue sur 1 000 sessions × 15,8 €) ≈
  −16 € pour 1 000 sessions ≈ **−230 € par an**. Un dixième de point
  d'abandon efface presque un point entier d'ajout.
- Le levier principal est donc l'**adoption** (nombre de sessions exposées,
  donc d'établissements avec SNO, borne ou barre POS active), puis l'absence
  de friction. Les chantiers doivent rester légers ; les modèles adaptatifs
  (U-17, U-18, U-19) sont hors de portée à ce volume.

### Une option de friction chiffrée, à tester seulement

« N'afficher le popup que si une association existe » : chez 212 et 226, l'étape
disparaîtrait pour 61 % des sessions et l'on perdrait 37 % des ajouts ; chez
231, on en perdrait 90 %. L'effet dépend de l'établissement : à ne jamais
généraliser sans test.

### Priorités révisées

Détail dans [L5](L5-backlog-opportunites.md) et [L6](L6-plan-action.md) :

1. **Fiabiliser la mesure** (U-01, C-22) et **corriger le POS** (U-27 : n'appeler
   que si la barre est affichée, suivre les mises à jour de commande).
2. **Exclusion d'un produit par le marchand** (U-26), demandée.
3. **Classement unique calibré** sur les ajouts observés, diversité, puis CA
   espéré et variante la plus vendue plutôt que la moins chère (U-09, U-10,
   U-12 révisé, sonde U-22a) : peu de code, aligné sur l'objectif CA.
4. **Friction** : seuil d'affichage (U-13) ou suggestions intégrées au panier,
   sans étape (U-28), en test.
5. Journal d'exposition et témoin léger (U-04, U-03), comme filet.
