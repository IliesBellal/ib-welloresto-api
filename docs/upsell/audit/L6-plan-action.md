# Livrable 6 — Plan d'action

**Révisé le 2026-10-16** ([L9](L9-resultats-baseline-prod.md)). Cadre : un
développeur, trois applications clientes, migrations appliquées par Ilies,
objectif CA avec le moins de friction possible, ~280 sessions upsell par
semaine sur SNO et borne (212, 226, 231), barre POS active chez 212 et 241.

## 0 à 30 jours — Réparer la mesure, livrer les correctifs bon marché

| Semaine | Action | Opportunités | Qui | Sortie attendue |
|---|---|---|---|---|
| 1 | Lancer B8 (état des commandes des acceptations) | — | Ilies | ampleur de C-22 |
| 1-2 | Indicateurs : acceptation vide, acceptation sur commande non payée, effacement POS, libellé | U-01 | dev | onglet Analytics juste |
| 1-2 | POS : appel seulement si la barre est affichée ; suivi en mise à jour de commande | U-27 | dev | lignes POS = expositions réelles ; acceptations POS enfin suivies |
| 1-2 | Disponibilité par mode dans le moteur ; plafond d'affichage par canal | U-05, U-06 | dev | listes complètes |
| 1-2 | Exclusion d'un produit par le restaurateur | U-26 | Ilies (migration), dev | demande de la question 5 livrée |
| 2-3 | Journal d'exposition : migration, colonnes, Tracker par ligne ; SNO puis borne envoient l'identifiant dans tous les cas | U-04 | Ilies (migration), dev | [L8 R1](L8-recommandations-techniques.md#r1--journal-dexposition-et-indicateurs-fiables) |
| 3-4 | Couche de politique (à 0 %), classement `unified_v1` (à 0 %), calibration nocturne, supervision du cron | U-03, U-09, U-10, U-07 | Ilies (migration), dev | [L8 R2](L8-recommandations-techniques.md#r2--groupe-témoin-et-couche-de-politique), [R3](L8-recommandations-techniques.md#r3--score-commun-ca-espéré-et-diversité) |
| 4 | Activation : témoin 20 %, `unified_v1` 40 %, contrôle 40 % sur 212, 226, 231 | T1, T2 | Ilies | premières données |

**Porte 1 (fin du mois)** : critères de R1 tenus (B8 propre, contexte écrit
sur toutes les lignes), équilibre des bras correct, aucune étape affichée dans
le témoin.

## 30 à 90 jours — Lire les deux premiers tests, préparer la friction

| Période | Action | Opportunités | Sortie attendue |
|---|---|---|---|
| J30-J100 | **T1** : témoin 20 % (filet contre +3 points d'abandon) | U-03 | abandon par bras ; CA upsell par session |
| J30-J115 | **T2** : `unified_v1` contre classement actuel (environ 12 semaines, le témoin prenant 20 % des sessions) | U-09, U-10 | sessions avec ajout payé par bras |
| J30-J60 | POS : mesurer, avec U-27 livré, les expositions réellement affichées et les ajouts chez 212 et 241 | U-27 | décider s'il vaut la peine d'activer la barre chez 236, 235, 234 (upsell activé, aucune exposition aujourd'hui) |
| J45-J75 | Seuil d'affichage par établissement (code à 0 %) | U-13 | prêt pour T3 |
| J60-J90 | Décision « Reporté » : exclusion des lignes `is_upsell`, des marketplaces et des annulations des statistiques | U-16 | décision consignée |
| J60-J90 | Cadrage de U-28 (suggestions dans le panier SNO et borne) selon les premières lectures de T1 | U-28 | maquettes, estimation |

**Porte 2 (vers J115)** :

- abandon en hausse de plus de 3 points dans T1 → priorité à la friction
  (T3, T4) avant tout autre changement de classement ;
- `unified_v1` gagnant dans T2 → il devient la politique par défaut, puis sonde
  « variante la plus vendue » (U-22a) ;
- T2 non concluant → garder le classement actuel, passer à la friction.

## 90 jours et plus — Selon les résultats

| Condition | Action | Opportunités |
|---|---|---|
| Porte 2 passée | **T3** : seuil d'affichage contre affichage systématique | U-13 |
| Objectif de friction prioritaire, ou abandon préoccupant | **T4** : suggestions dans le panier, sans étape, contre popup | U-28 |
| Calibration par tranche de prix assez fournie (300 présentations et 5 ajouts par case) | `unified_v2` (CA espéré) contre la politique gagnante | U-12 |
| Sonde U-22a positive | Montée en gamme (« remplacer par ») | U-22b |
| Objectif déplacé vers la marge | Saisie des coûts des produits suggérés, puis variante marge | U-11 |
| Adoption forte (plusieurs milliers de sessions par semaine) | Taux d'acceptation par produit, incrémentalité par segment, bandit entre politiques | U-17, U-18, U-19 |

**Le levier principal reste l'adoption** : à 280 sessions par semaine, le CA
upsell attribué payé est d'environ 1 500 € TTC par an tous établissements
confondus. Chaque établissement qui active SNO, une borne ou une barre POS
effectivement utilisée pèse plus que n'importe quel réglage du classement.

**Évolution architecturale** : le découpage ciblé (U-23) se fait au fil de R2
et R3. Une refonte n'est pas justifiée.

## Ce qu'il ne faut pas faire

- Réactiver le LLM pour choisir les produits (coût, latence, aucune preuve de
  gain).
- Investir dans des modèles adaptatifs (bandits, uplift, apprentissage par
  produit) : hors de portée à ce volume.
- Classer par « probabilité brute × prix » sans calibration : les scores bruts
  surestiment l'ajout d'un ordre de grandeur et pousseraient en tête des
  articles chers peu achetés.
- Conclure d'une comparaison avant/après : 0,10 € par session ne se voit pas
  dans le panier moyen.
- Activer la barre POS ailleurs avant U-27 : on ajouterait des appels sans
  affichage et des acceptations non suivies.
- Piloter sur le « taux de transformation » actuel (C-02, C-03, C-22).
