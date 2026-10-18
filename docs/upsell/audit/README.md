# Audit du moteur d'upsell — benchmark Dynamic Yield (2026-10-16)

Audit stratégique et technique du moteur de vente additionnelle (POS, ScanNOrder,
borne), orienté **gain incrémental** : la marge dans le cahier des charges,
le CA depuis l'arbitrage du 2026-10-16. Mode lecture seule : aucun code
modifié, rien de commité. Seul livrable exécutable : un fichier de requêtes en
lecture seule à lancer sur la prod.

## Livrables

| # | Fichier | Contenu |
|---|---|---|
| 1 | [L1-synthese-executive.md](L1-synthese-executive.md) | Une page : constats, faiblesses, opportunités, risques, trois premières actions |
| 2 | [L2-cartographie-moteur.md](L2-cartographie-moteur.md) | Architecture réelle, flux de décision, dépendances, zones non vérifiées |
| 3 | [L3-matrice-maturite.md](L3-matrice-maturite.md) | Matrice comparative (18 capacités), faits Dynamic Yield sourcés, limites du benchmark |
| 4 | [L4-analyse-critique-code.md](L4-analyse-critique-code.md) | 21 constats référencés (fichier:ligne), impact, correction ; avis sur les points « Reporté » |
| 5 | [L5-backlog-opportunites.md](L5-backlog-opportunites.md) | 25 opportunités classées A/B/C/D, priorisées gain × confiance / effort |
| 6 | [L6-plan-action.md](L6-plan-action.md) | 0-30 j, 30-90 j, 90 j et plus, avec portes de décision |
| 7 | [L7-plan-mesure-profit.md](L7-plan-mesure-profit.md) | Définitions, événements, plans de test, tailles d'échantillon, simulation économique |
| 8 | [L8-recommandations-techniques.md](L8-recommandations-techniques.md) | Les trois chantiers techniques prioritaires : composants, pseudo-code, tests, déploiement, retour arrière ; exclusion marchand en annexe |
| 9 | [L9-resultats-baseline-prod.md](L9-resultats-baseline-prod.md) | **Résultats de prod (B0 à B7) et réponses aux cinq questions** : ce qui a été mesuré et ce que cela change |
| — | [requetes-baseline-prod.sql](requetes-baseline-prod.sql) | 9 requêtes agrégées : B0 à B7 (exécutées le 2026-10-16), B8 ajoutée ensuite (état des commandes des acceptations) |

**Mise à jour du 2026-10-16** : les livrables 1, 4 à 8 ont été révisés avec
les données de prod et les réponses aux questions ; L9 en donne le détail et
prime en cas d'écart. Objectif arbitré : **le CA, avec le moins de friction
possible**.

## Matériel examiné

**Code (lu intégralement pour le cœur, par extraits ciblés ailleurs)**

- API : [internal/modules/upsell/](../../../internal/modules/upsell/) (service, repository,
  tracker, types, prompts, tests), [internal/tasks/upsell.go](../../../internal/tasks/upsell.go)
  (calcul nocturne), `menu.ListAvailableProductsForUpsell`
  ([menu/repository.go:107-216](../../../internal/modules/menu/repository.go#L107-L216)),
  les trois points d'entrée ([orders/handler.go:128](../../../internal/modules/orders/handler.go#L128),
  [scannorder/service.go:1609](../../../internal/modules/scannorder/service.go#L1609),
  [kiosk/service.go:1631](../../../internal/modules/kiosk/service.go#L1631)), le
  déclenchement du suivi ([order_life_cycle/service.go:1164](../../../internal/modules/order_life_cycle/service.go#L1164)),
  l'onglet Analytics ([analytics/upsell.go](../../../internal/modules/analytics/upsell.go)),
  le coût de revient figé ([order_life_cycle/cost.go](../../../internal/modules/order_life_cycle/cost.go),
  migration 114), la journalisation des requêtes
  ([request_logger/middleware.go](../../../internal/middleware/request_logger/middleware.go)),
  la configuration IA ([config/ai.go:39-49](../../../internal/config/ai.go#L39-L49)),
  la planification ([cmd/api/tasks.go:61](../../../cmd/api/tasks.go#L61)).
- Clients : POS Flutter (`upsell_controller.dart`, `upsell_suggestions_bar.dart`,
  `order_controller.dart`), borne (`upsell_controller.dart`, `upsell_screen.dart`,
  `cart_screen.dart`, `app_config.dart`), ScanNOrder (`useUpsell.ts`, `upsell.ts`,
  `UpsellPopup.tsx`).

**Documentation** : [UPSELL_COMPLETION.md](../../UPSELL_COMPLETION.md) (décisions D1 à
D13, résultats de prod du 2026-09-27 et 2026-09-28), [audit de traçabilité du
2026-07-02](../../audits/audit_upsell_traceability.md), [diagnostic du 2026-09-05](../../audits/2026-09-05-upsell-diagnostic-prompt20.md),
entrées upsell de [decisions.md](../../decisions.md), requêtes de prod existantes.

**Données** : première version rédigée sans données nouvelles, à partir des
chiffres de prod consignés dans [UPSELL_COMPLETION.md](../../UPSELL_COMPLETION.md)
§6 et §8. Le 2026-10-16, Ilies a exécuté B0 à B7 sur la prod ; analyse dans
[L9](L9-resultats-baseline-prod.md). La sortie de B7 est coupée à 200 lignes
(limite d'export), sans effet sur les conclusions SNO et borne. Les chiffres de
staging présents dans d'autres documents ne sont **pas** utilisés.

**Encore inconnu** (chaque conclusion qui en dépend reste conditionnelle) :

- l'effet de l'étape upsell sur l'abandon (aucun témoin) ;
- l'état des commandes portant une acceptation (B8, à lancer) ;
- le nombre de suggestions POS réellement affichées (C-23) ;
- les coûts : le mécanisme existe, aucun produit suggéré n'a de coût saisi.

## Conventions

- **Confiance** : *Élevée* = vérifié dans le code ou dans un chiffre de prod
  consigné ; *Moyenne* = déduit du code et de la documentation, non mesuré ;
  *Faible* = hypothèse.
- **Nature d'une affirmation** (benchmark) : **[A]** fait documenté par une
  source publique ; **[B]** pratique industrielle reconnue ; **[C]** hypothèse
  technique ; **[D]** opportunité pour WelloResto.
- **Maturité** : échelle d'audit 0 à 5 (définie dans [L3](L3-matrice-maturite.md)).
  Ce n'est pas une échelle de Dynamic Yield.
- Les montants hypothétiques sont toujours signalés comme tels.

## Les cinq questions qui changeraient le plus les recommandations

1. **Volume** — combien de sessions éligibles par semaine, par canal, chez les
   établissements où l'upsell est (ou sera) activé ? En dessous de quelques
   milliers de sessions par mois et par test, seules des règles simples et un
   groupe témoin commun à plusieurs établissements sont mesurables ; les
   modèles adaptatifs (bandits, uplift) ne le sont pas. *Réponse : requêtes B0
   et B6.*
2. **Coûts** — les restaurateurs peuvent-ils saisir un coût d'achat pour les
   produits revendus, ou un taux de marge par défaut (par établissement ou par
   catégorie) est-il une approximation acceptable ? Sans l'un des deux, un
   classement « par marge » est impossible et l'on optimise au mieux le CA.
   *Réponse partielle : requête B4.*
3. **Objectif** — que maximise-t-on : la marge du restaurateur, son CA, ou le
   revenu de WelloResto (si une part dépend du volume encaissé) ? Et quelle
   friction est acceptable (un popup avant chaque paiement sur SNO et la borne
   est-il voulu) ?
4. **État déployé** — depuis quand D1 à D13 tournent-ils en prod, quelles
   versions des clients sont en salle (popup SNO avec `isUpsell`, écran borne,
   barre POS), et la barre POS (tablette uniquement) est-elle réellement
   utilisée par le personnel ? Cela fixe la fenêtre de référence et l'inclusion
   ou non du POS dans les tests.
5. **Contraintes opérationnelles et contrôle marchand** — les cuisines
   saturent-elles aux heures de pointe (au point qu'un dessert ou un
   accompagnement suggéré dégrade le service) ? Les restaurateurs veulent-ils
   épingler ou exclure des produits ? Cela décide s'il faut un signal de
   charge et des réglages marchand, deux chantiers coûteux.

**Réponses d'Ilies (2026-10-16)** et conséquences, détaillées en [L9 §0](L9-resultats-baseline-prod.md#0-réponses-et-conséquences) :

1. Volume : données des requêtes → ~280 sessions upsell par semaine sur SNO et
   borne ; seuls de gros effets sont mesurables.
2. Coûts : saisissables via le coût d'achat des composants → pas de nouvelle
   colonne ; mais 0 % de couverture aujourd'hui pour les produits suggérés.
3. Objectif : **le CA**, le moins de friction possible → indicateur principal
   = CA incrémental par session ; classement par probabilité calibrée puis CA
   espéré ; la friction devient un critère de premier rang.
4. Tout est en prod et utilisé, selon le personnel → fenêtre de référence au
   2026-09-28 ; la barre POS est en pratique très peu utilisée.
5. Possibilité de retirer un article des suggestions, rien d'autre → U-26 ;
   pas de signal de charge cuisine.

## Sources publiques (Dynamic Yield, McDonald's)

Utilisées dans [L3](L3-matrice-maturite.md). Les pages d'aide de Dynamic Yield
ont refusé la lecture directe (HTTP 403) : leur contenu est repris de résumés
de moteur de recherche, d'où une confiance *moyenne* sur les détails de
paramétrage.

- [Retail TouchPoints — acquisition par McDonald's (2019)](https://retailtouchpoints.com/features/news-briefs/mcdonald-s-acquires-personalization-provider-dynamic-yield-for-300-million/)
- [Hospitality Technology — annonce de l'acquisition (2019)](https://hospitalitytech.com/mcdonalds-acquire-dynamic-yield)
- [Fox Business — usage prévu au drive (2019)](https://foxbusiness.com/markets/mcdonalds-just-spent-300-million-on-new-drive-thru-tech)
- [Engadget — suggestions plus simples à préparer en cas d'affluence (2019)](https://www.engadget.com/2019/03/26/mcdonalds-ai-drive-thru-machine-learning-dynamic-yield/)
- [Restaurant Dive — résultats T2 2019, déploiement vers 8 000 drives](https://www.restaurantdive.com/news/mcdonalds-revs-up-personalization-rollout-to-8k-drive-thrus/559649/)
- [QSR Magazine — hausse du ticket moyen évoquée (2019)](https://www.qsrmagazine.com/fast-food/mcdonalds-new-tech-about-change-restaurant-industry)
- [McDonald's — faits marquants T2 2019](https://corporate.mcdonalds.com/corpmcd/our-stories/article/q2_2019_highlights.html)
- [invidis — résultats T4 2019, plus de 11 000 drives](https://invidis.com/2020/03/02/mcdonalds-confirms-sales-lifts-from-ai-driven-suggestive-selling-in-drive-thru-lanes/)
- [Nation's Restaurant News — cession à Mastercard annoncée (déc. 2021)](https://www.nrn.com/quick-service/mcdonald-s-agrees-to-sell-dynamic-yield-to-mastercard)
- [Nation's Restaurant News — cession finalisée (avr. 2022)](https://nrn.com/quick-service/mcdonald-s-completes-sale-dynamic-yield-mastercard)
- [Restaurant Dive — cession à Mastercard](https://www.restaurantdive.com/news/mcdonalds-sells-ai-tech-firm-dynamic-yield-to-mastercard/616412/)
- [Dynamic Yield — modes d'allocation (A/B, dynamique, affinité)](https://support.dynamicyield.com/hc/en-us/articles/360022065293)
- [Dynamic Yield — « Probability to Be Best »](https://support.dynamicyield.com/hc/en-us/articles/360011253618)
- [Dynamic Yield — calculateur bayésien](https://marketing.dynamicyield.com/bayesian-calculator/)
- [Dynamic Yield — notes de version 2019 (« achetés ensemble »)](https://support.dynamicyield.com/hc/en-us/articles/38951169280285-Release-Notes-2019)
- [Mastercard / Dynamic Yield — affinité](https://www.mastercard.com/us/en/business/consumer-acquisition-and-engagement/personalization/dynamic-yield/solutions/affinity.html)
- [Mastercard / Dynamic Yield — ciblage](https://www.mastercard.com/us/en/business/consumer-acquisition-and-engagement/personalization/dynamic-yield/use-cases/targeting.html)
- [Mastercard / Dynamic Yield — capacités de recommandation (2022)](https://www.mastercard.com/us/en/news-and-trends/Insights/2022/an-overview-of-dynamic-yields-essential-recommendation-capabilities.html)
