# ScanNOrder place de marché

Objectif d'Ilies, à terme : faire de ScanNOrder une place de marché de commande en ligne, avec les fonctionnalités d'Uber Eats, une fois la base de restaurants suffisante. Rien n'est à construire tout de suite. Ce document fixe les décisions déjà prises et les règles à respecter d'ici là, pour ne pas se fermer de portes. Document de référence pour l'API, le back-office et ScanNOrder.

Décisions prises avec Ilies le 2026-10-27.

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| M1 | **Pas de livraison par la plateforme pour l'instant.** Les établissements qui livrent le font eux-mêmes, comme aujourd'hui (frais, minimum, vérification d'adresse par établissement). | Décision Ilies. |
| M2 | **Paiement : Stripe Connect, tel qu'aujourd'hui.** Le client paie l'établissement, sur son compte Stripe ; la plateforme prélève ses frais d'application (`ApplicationFeeAmount`, `internal/infrastructure/stripe/checkout.go`). Si la plateforme livre un jour, le coût de la livraison est couvert en ajustant ces frais d'application. Pas de passage à un modèle où la plateforme encaisse puis reverse. | Stripe répartit l'argent automatiquement, sans reversements à gérer (décision Ilies). |
| M3 | **Compte client unique pour tous les établissements**, une fiche client séparée chez chaque établissement, fidélité par établissement. | Déjà la conception des comptes ScanNOrder (`wello-resto-scannorder/docs/comptes-client.md`) : c'est le modèle d'une place de marché. |
| M4 | **Adresses publiques neutres** : `/restaurant/{slug}` pour un établissement, `/hub/{slug}` pour une enseigne à plusieurs établissements (franchises.md, D7). | Une place de marché a des pages d'établissement et des pages de marque ; « franchise » ne convient pas à toutes les enseignes. |
| M5 | **Zones de livraison par établissement**, quand elles seront faites (prévues en v2 du hub, franchises.md) : zones géographiques rattachées à l'établissement et interrogées côté serveur, pas un réglage de la franchise. | C'est la requête centrale d'une place de marché : « qui livre à mon adresse ». |

## Conséquences acceptées

- **M2, promotions** : une promotion financée par la plateforme ne peut pas dépasser ses frais d'application sur la commande (elle se fait en les réduisant). Les promotions financées par l'établissement (module remises) ne sont pas concernées.
- **M2, remboursements** : ils partent du compte de l'établissement, comme aujourd'hui.
- **M2, livreurs** : si la plateforme livre un jour avec ses propres livreurs, leur paiement sort des frais d'application perçus par la plateforme. Avec un service de livraison tiers (Uber Direct, Stuart…), la plateforme règle ce service et récupère le coût par les frais d'application.

## Règles à respecter d'ici là

1. Ne pas construire de fonction qui suppose que la plateforme encaisse l'argent des commandes (M2).
2. Ne pas rattacher à la franchise ce qui relève de l'établissement : livraison, horaires, menu, frais (M5). La franchise regroupe, elle ne remplace pas l'établissement.
3. Garder les données client cloisonnées par établissement côté back-office, et le compte client commun côté client (M3).
4. Pas de nouvelle adresse publique contenant un statut commercial ou un terme interne (M4).
5. La mesure d'audience reste anonyme ; toute personnalisation (« commander à nouveau », recommandations) passera par le compte client, et la publicité par le module de consentement (mesure-audience.md, D23).

## À faire le jour venu (rien de bloquant dans la base actuelle)

| Sujet | Ce qu'il faudra | Ampleur |
|---|---|---|
| Découverte | Page d'accueil listant les établissements autour d'une adresse, recherche (nom, cuisine, plat), filtres. Recherche côté serveur avec un index géographique ; le calcul de distance dans le navigateur (franchises.md, D8) reste valable pour le hub. | Élevée |
| Ouvert / fermé | Aujourd'hui calculé à chaque requête et par établissement : à précalculer pour lister des centaines d'établissements. | Moyenne |
| Comptes client | Mettre en œuvre `docs/comptes-client.md` (ScanNOrder) : historique tous établissements, favoris, « commander à nouveau ». | Élevée |
| Avis et notes | Avis vérifiés (liés à une commande), modération. | Moyenne |
| Abonnement client (type Uber One) | Frais de livraison ou de service réduits ; financé par la plateforme, donc dans la limite de M2. | Moyenne |
| Support | Réclamations et remboursements depuis la plateforme. | Moyenne |
| Application mobile | Application client iOS / Android. | Élevée |
| Marque et domaine | « ScanNOrder » évoque la commande par QR code : une place de marché aura sans doute son propre nom et domaine. Les adresses `/restaurant/` et `/hub/` se transportent, avec redirections 301 pour le référencement. | Faible à moyenne |
| Livraison (si M1 change) | Intégrer un service de livraison tiers avant d'envisager une flotte propre ; le cycle de vie des commandes devra accepter le statut d'un livreur externe. | Élevée |
| Juridique | Statut d'intermédiaire : règlement européen P2B (transparence envers les restaurants), DSA (vérification des professionnels, avis), TVA sur les frais d'application, conditions générales de la place de marché. | À cadrer avec un juriste |
