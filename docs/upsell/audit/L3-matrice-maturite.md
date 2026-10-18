# Livrable 3 — Matrice de maturité et benchmark Dynamic Yield

## 1. Échelle utilisée

Référentiel d'audit proposé pour ce document, **pas une échelle de Dynamic
Yield** :

| Niveau | Définition |
|---|---|
| 0 | Absent ou non observable |
| 1 | Règles statiques élémentaires |
| 2 | Règles structurées et contextualisées |
| 3 | Scoring ou ranking optimisé |
| 4 | Personnalisation prédictive mesurée |
| 5 | Optimisation incrémentale continue, pilotée par l'expérimentation |

« Non déterminé » signifie que les éléments fournis ne permettent pas de
conclure, pas que la capacité est absente.

## 2. Matrice comparative

Légende des colonnes 4 et 5 : **[B]** pratique industrielle, **[A]** fait
documenté (sources dans le [README](README.md#sources-publiques-dynamic-yield-mcdonalds)).
Gain et difficulté : F (faible), M (moyen), É (élevé). Les identifiants U-xx
renvoient au [backlog](L5-backlog-opportunites.md).

| # | Capacité | État actuel WelloResto | Maturité | Pratique de référence [B] | Dynamic Yield, documenté [A] | Écart ou information manquante | Amélioration | Gain | Difficulté | Confiance |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | Génération des candidats et filtres métier | Catalogue disponible, sans groupes ni variantes sœurs, hors panier ; canal et horaires filtrés avant sélection (SNO, borne) | 2 | Tous les filtres de contexte appliqués avant le classement | Filtres de recommandation et règles de filtre personnalisées (base de connaissances) | Disponibilité par mode de commande filtrée **après** le moteur ; places perdues | U-05 | F-M | F | Élevée |
| 2 | Compatibilité et exclusions | Pas de groupe ; pas de catégorie déjà au panier pour les associations | 2 | Règles d'exclusion explicites, réglages marchand (épingler, exclure) | Règles de filtre personnalisées | Aucun réglage marchand (exclusion demandée, question 5) ; règle de catégorie absente du complément | U-26, U-10 | M | F | Élevée |
| 3 | Complémentarité (achetés ensemble) | Règles d'association nocturnes, P(B\|A) lissée, lift, support, par groupe | 2-3 | Règles d'association ou filtrage article-article, avec lissage | Algorithmes « achetés ensemble » (notes de version 2019) | Paniers de tous canaux mélangés, lignes upsell incluses (boucle), annulations non exclues | U-16 | M | F | Élevée |
| 4 | Scoring et classement | Priorité figée par étape ; scores non comparables (0,10-1 / 0,5 / auto-déclaré) | 2 | Un score unique et comparable (probabilité × valeur) | Stratégies d'apprentissage automatique et d'affinité ; algorithmes internes non publics | Pas d'échelle commune ; le moins cher gagne à égalité | U-09, U-12 | M-É | M | Élevée |
| 5 | Prise en compte de la valeur (CA, marge) | Absente ; objectif arbitré : CA ; coût saisissable mais 0 % des produits suggérés couverts (prod) | 0-1 | Valeur espérée = probabilité calibrée × prix (ou marge) | Non documenté (ni pour la plateforme, ni pour McDonald's) | Scores non calibrés ; aucun coût saisi | U-09, U-12, U-11 | M | M | Élevée |
| 6 | Incrémentalité | Non mesurée et non mesurable | 0 | Groupe témoin permanent, tests A/B, uplift | Tests A/B, allocation dynamique (bandit multi-bras), moteur bayésien « Probability to Be Best » | Aucun témoin, exposition non journalisée | U-03, U-18 | É (indirect) | M | Élevée |
| 7 | Contexte : composition du panier | Oui : associations depuis chaque article, catégories du panier, variantes | 2-3 | Idem, plus quantités et taille du panier | Suggestions selon les articles déjà commandés (McDonald's, 2019) | Quantités ignorées | — | F | F | Élevée |
| 8 | Contexte : moment, météo, affluence | Non ; le type de commande ne sert qu'à la clé de cache et au prompt LLM | 1 | Créneau horaire si le volume le permet ; charge de cuisine | Heure, météo, affluence, tendances (annonce McDonald's 2019) ; articles plus simples aux heures de pointe (rapporté par la presse) | Aucun signal de charge ni de créneau | U-20, U-21 | Incertain | M-É | Moyenne |
| 9 | Personnalisation par client | Aucune (SNO et borne largement anonymes) | 0 | Seulement si le client est identifié et consentant | Profils d'affinité par utilisateur (plateforme) | Peu pertinent à court terme | — | F | É | Moyenne |
| 10 | Montée en gamme (taille, menu, option) | Absente ; variante **la moins chère** choisie volontairement | 0 | Suggestions de remplacement par une version supérieure | Non documenté | Type de suggestion inexistant côté moteur et clients | U-22 | É (hyp.) | É | Moyenne |
| 11 | Diversité et répétition | Dédoublonnage ; pas de plafond par catégorie ; même liste pour un même panier | 1 | Re-classement par diversité, rotation contrôlée | Allocation dynamique avec part de trafic aléatoire (10 % selon la base de connaissances) | ~4 boissons sur 6 estimées chez 212 | U-10 | M | F | Moyenne |
| 12 | Décider de ne rien proposer | Étape sautée seulement si aucun candidat ; popup avant 99,8 % des paiements SNO et borne (prod) | 1 | Seuil de valeur espérée, ou affichage sans étape bloquante | Non documenté | Friction non mesurée, contraire à l'objectif | U-13, U-28 | M (via l'abandon) | F-M | Élevée |
| 13 | Expérimentation | Aucune | 0 | A/B randomisé, groupe témoin permanent, règles d'arrêt définies à l'avance | A/B, allocation dynamique, durée minimale par défaut de 14 jours, score bayésien | Tout | U-03 | É (indirect) | M | Élevée |
| 14 | Boucle d'apprentissage | Apprend des ventes chaque nuit, pas des expositions ; boucle auto-renforçante | 1 | Apprentissage sur expositions et acceptations, avec lissage | Allocation selon la performance observée | Pas d'événement d'exposition | U-04, U-17 | M | M | Élevée |
| 15 | Instrumentation et traçabilité | Une ligne par appel (source, origine, score) ; `is_upsell` sur 3 canaux ; journal des requêtes 90 j ; suivi POS jamais déclenché, acceptations avant paiement (prod) | 1 | Un événement par exposition, avec contexte, bras et version | Rapports d'expérience et métrique principale (base de connaissances) | Contexte, affichage réel et bras absents ; indicateurs biaisés (C-02, C-03, C-22, C-23) | U-01, U-04, U-27 | É (indirect) | F | Élevée |
| 16 | Restitution économique | CA upsell attribué HT/TTC, taux, top produits | 1-2 | Marge incrémentale par session éligible, avec intervalle de confiance | Rapports d'expérience | Ni marge, ni contrefactuel | U-11, [L7](L7-plan-mesure-profit.md) | É (indirect) | F-M | Élevée |
| 17 | Robustesse et latence | Replis, `recover`, cache, délais clients de 2 s ; prod : p95 690-835 ms, 0,4 % au-delà de 2 s | 3 | Objectif de latence suivi, alertes | Non documenté | Cron non supervisé | U-07 | F | F | Élevée |
| 18 | Présentation et accroche | Accroche générée, jamais affichée | 0-1 | Raison contextuelle testée (« souvent pris avec… ») | Non documenté | Champ inutilisé | U-14 | Incertain | F-M | Élevée |

## 3. Profil de maturité

Pas de note globale : une moyenne mélangerait des capacités de poids
économique très différent. Profil par niveau (la borne basse est retenue quand
une capacité est notée « 2-3 » ou « 0-1 ») :

| Niveau | Capacités |
|---|---|
| 0 | Valeur, CA ou marge (5), incrémentalité (6), client (9), montée en gamme (10), expérimentation (13), accroche (18) |
| 1 | Contexte temporel (8), diversité (11), « ne rien proposer » (12), apprentissage (14), instrumentation (15), restitution (16) |
| 2 | Candidats (1), exclusions (2), associations (3), classement (4), panier (7) |
| 3 | Robustesse et latence (17), mesurée en prod le 2026-10-16 |
| 4 et plus | aucune |

Lecture : le moteur est **solide au niveau 2** sur tout ce qui relève de la
règle bien construite, et **au niveau 0 ou 1** sur tout ce qui relève de la
mesure et de la valeur économique. Or ce sont ces dernières capacités qui
décident si l'upsell rapporte.

## 4. Faits documentés sur Dynamic Yield et McDonald's [A]

| Fait | Source |
|---|---|
| McDonald's annonce l'acquisition de Dynamic Yield en mars 2019 ; prix non communiqué officiellement, environ 300 M$ selon la presse | Retail TouchPoints, Fox Business |
| Usage annoncé : faire varier les menus numériques des drives selon l'heure, la météo, l'affluence du restaurant et les articles tendance, et suggérer des ajouts selon les articles déjà choisis ; extension prévue aux bornes et à l'application | Hospitality Technology, Retail TouchPoints |
| Rapporté par la presse : aux heures de pointe, suggestion d'articles plus simples à préparer | Engadget |
| T2 2019 : « logique de décision » déployée dans plus de 700 drives américains, panier moyen en hausse (aucun chiffre publié), déploiement annoncé vers environ 8 000 restaurants | Restaurant Dive, QSR Magazine, McDonald's |
| T4 2019 : technologie présente dans plus de 11 000 drives, contribuant à la hausse du panier moyen ; hausse des ventes comparables « conforme au modèle » de l'acquisition (aucun chiffre publié) | invidis |
| Décembre 2021 : cession à Mastercard annoncée ; finalisée en avril 2022 ; McDonald's indique continuer à travailler avec Dynamic Yield, intégré à ses drives et bornes dans plusieurs marchés | Nation's Restaurant News, Restaurant Dive |
| Plateforme : trois modes d'allocation — test A/B, allocation dynamique (bandit multi-bras, la variante la plus performante reçoit plus de trafic), allocation par affinité | Base de connaissances Dynamic Yield |
| Plateforme : moteur bayésien « Probability to Be Best », durée minimale de test par défaut de 14 jours, 10 % des sessions servies au hasard en allocation dynamique | Base de connaissances Dynamic Yield (lue via résumé, confiance moyenne) |
| Plateforme : algorithmes « achetés ensemble » à partir des achats en ligne et hors ligne (2019), filtres et règles de filtre personnalisées, profils d'affinité | Notes de version 2019, pages Mastercard |

**Ce qui n'est pas public** : l'architecture, les modèles et les règles de
classement utilisés par McDonald's ; la fonction objectif (CA, marge, vitesse
de service) ; l'ampleur mesurée des gains ; la manière dont l'incrémentalité y
est mesurée. Rien de cela n'est attribué à McDonald's dans cet audit.

## 5. Pratiques industrielles mobilisées [B]

- Règles d'association avec lissage pour les petits échantillons ; filtrage
  article-article.
- Classement par **valeur espérée** = probabilité d'acceptation × marge
  contributive, plutôt que par probabilité seule.
- **Groupe témoin permanent** (une petite part des sessions ne voit jamais
  l'upsell) pour mesurer la valeur totale du dispositif dans la durée.
- Tests A/B randomisés avec unité de randomisation stable, contrôle du
  déséquilibre des groupes (*sample ratio mismatch*), règles d'arrêt fixées à
  l'avance.
- Lissage bayésien (bêta-binomial) des taux d'acceptation par produit et par
  contexte quand les volumes sont faibles.
- Bandits (Thompson sampling) sur un **petit nombre de politiques** plutôt que
  sur des centaines de produits, quand le volume ne suffit pas.
- Re-classement par diversité et plafonds de répétition.
- Mesure de la friction (abandon, durée de passage) comme garde-fou de
  premier rang sur un parcours de paiement.

## 6. Hypothèses techniques [C]

Mécanismes **possibles** chez Dynamic Yield ou McDonald's, non attribuables
faute de preuve :

- la « logique de décision » évoquée au T2 2019 peut désigner un arbre de
  règles contextuelles plutôt qu'un modèle appris, au moins au départ ;
- l'arbitrage entre suggestions peut intégrer la vitesse de préparation (la
  presse le rapporte) et donc une contrainte opérationnelle explicite ;
- une métrique d'optimisation fondée sur le CA ou la conversion plutôt que sur
  la marge aurait le même angle mort que le moteur actuel.

## 7. Limites du benchmark et contraintes propres à WelloResto

- **Volume** [B] : bandits et sélection bayésienne demandent beaucoup de
  sessions par variante. McDonald's dispose de dizaines de milliers de
  restaurants au même catalogue ; WelloResto sert des indépendants, chacun avec
  son catalogue (8 établissements avec au moins 50 commandes clôturées sur 90
  jours au 2026-09-27). L'apprentissage par établissement sera presque
  toujours sous-dimensionné ; l'apprentissage commun suppose une taxonomie de
  rôles produits partagée (boisson, dessert…), que le rejet de P1 (pas de
  détection par mots-clés) laisse ouverte à un réglage marchand (U-24).
- **Objectif** [C] : rien ne dit que Dynamic Yield optimise la marge par défaut.
  Copier la plateforme sans définir la métrique reproduirait le problème.
- **Coût** : une plateforme tierce de personnalisation est dimensionnée et
  facturée pour des volumes sans rapport avec un indépendant ; l'intégration
  à trois clients et au calcul des prix serait lourde. Non recommandé.
- **Contexte de vente** : le drive McDonald's est un menu affiché unique et
  rapide ; ici, trois parcours différents (serveur, QR code en salle, borne)
  avec des moments d'exposition différents. Les leçons transposables sont les
  principes (contexte, contrainte opérationnelle, expérimentation), pas
  l'outillage.
