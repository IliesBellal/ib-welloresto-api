# Livrable 1 — Synthèse exécutive

*Révisée le 2026-10-16 avec les données de prod ([L9](L9-resultats-baseline-prod.md)).
Objectif arbitré : le CA, avec le moins de friction possible.*

**Verdict** : un moteur de règles robuste qui rapporte peu et dont la mesure
trompe. Sur SNO et la borne, 5,4 % des sessions aboutissent à un ajout payé,
soit 101 € TTC de CA attribué pour 1 000 sessions (environ 1 500 € par an pour
tous les établissements). Les priorités : fiabiliser la mesure, rendre la tête
de liste aux produits qui se vendent, réduire la friction. Pas de modèle
sophistiqué : le volume ne le permet pas.

## Cinq constats

1. **Rendement modeste, concentré sur les petits prix.** 37 sessions avec un
   ajout payé sur 683 (45 en comptant les commandes jamais payées), 1,87 € par
   session avec ajout, 0,5 % à 1,8 % du CA SNO + borne. *(Mesuré.)*
2. **La place de tête est mal employée.** Les associations passent toujours
   devant ; leur produit n'est ajouté que 2 fois sur 266 présentations, contre
   1,32 % pour les petits prix. Là où une association existe, 14 ajouts sur 15
   portent sur un petit prix plus bas dans la liste. *(Mesuré, petits
   effectifs.)*
3. **Friction maximale.** Le popup s'ouvre avant 99,8 % des paiements SNO et
   borne. Avec 0,10 € de CA payé par session, une hausse d'abandon d'environ
   0,5 à 0,6 point suffit à le rendre négatif, et ce seuil est trop bas pour
   être mesuré à ~280 sessions par semaine. *(Mesuré, sauf l'abandon : inconnu.)*
4. **Les indicateurs trompent.** Acceptations enregistrées avant paiement (8 sur
   45 ne correspondent à aucune vente), acceptations vides
   comptées, une ligne par appel. Au POS, le moteur est appelé même sur
   téléphone où la barre n'existe pas (3,1 appels par commande chez 212) et
   aucune acceptation n'est jamais suivie. *(Mesuré et vérifié dans le code.)*
5. **Rien ne mesure la marge.** Aucun produit suggéré n'a de coût saisi chez
   les établissements réels ; la latence, elle, est bonne (p95 sous 0,85 s).
   *(Mesuré.)*

## Trois faiblesses majeures

1. **Mesure fausse ou absente** (C-01, C-02, C-03, C-22, C-23).
2. **Classement par étape et non par valeur** (C-08, C-09).
3. **Une étape bloquante avant presque chaque paiement**, contraire à
   l'objectif de friction (C-12).

## Cinq opportunités prioritaires

| # | Opportunité | Mécanisme | Réf. |
|---|---|---|---|
| 1 | Indicateurs et POS réparés | Compter seulement les ventes réelles ; POS : n'appeler que si la barre est affichée, suivre les ajouts sur commande existante | U-01, U-27 |
| 2 | Classement unique calibré + diversité | La tête de liste au produit le plus probable, sur une échelle commune corrigée par les ajouts observés | U-09, U-10 |
| 3 | Exclusion d'un produit par le restaurateur | Retirer les suggestions incongrues sans règle sur les noms (demande) | U-26 |
| 4 | Moins de friction | Sauter l'étape quand elle rapporte peu, ou suggestions dans le panier sans popup | U-13, U-28 |
| 5 | CA par ajout | Variante la plus vendue plutôt que la moins chère ; CA espéré une fois la calibration assez fournie | U-22a, U-12 |

## Risques majeurs

- **Abandon invisible** : seul un écart de 3 points se lit en ~10 semaines ;
  un écart de 0,5 point, déjà coûteux, resterait indétectable.
- **Petits volumes** : tout test ne lira que de gros effets (+50 % d'ajouts en
  10 à 12 semaines).
- **Scores bruts mal calibrés** : classer par « score × prix » sans
  correction pousserait des articles chers peu achetés.
- **Boucle auto-renforçante** : les ventes issues de suggestions nourrissent
  les statistiques qui les produisent.

## Trois premières actions

1. **Cette semaine** : corriger les indicateurs et le POS (U-01, U-27) ;
   livrer l'exclusion marchand (U-26).
2. **Sous un mois** : journal d'exposition, couche de politique, témoin 20 % et
   `unified_v1` en test sur 212, 226 et 231 ([L8](L8-recommandations-techniques.md)).
3. **Ensuite** : selon les résultats, attaquer la friction (seuil, ou
   suggestions sans étape) et l'adoption (barre POS réellement utilisée, plus
   d'établissements sur SNO et borne), qui pèse plus que tout réglage.

Le benchmark Dynamic Yield ([L3](L3-matrice-maturite.md)) confirme la
direction : contexte, contrainte opérationnelle et expérimentation. Il ne
documente ni la fonction objectif de McDonald's ni l'ampleur des gains, et
son outillage suppose des volumes sans rapport avec ceux d'un indépendant.
