# Compte client : travaux futurs

Écartés du chantier « compte client » le 2026-11-17 (décision d'Ilies), à
reprendre plus tard. Contexte : `docs/customer-accounts.md`.

## Facture récapitulative mensuelle (clients professionnels)

- Une même vente ne peut pas avoir deux factures. CGI art. 289 : une facture
  récapitulative mensuelle est possible, mais **à la place** des factures à
  l'unité des commandes concernées.
- Piste : option « Facturé au mois » sur la fiche d'un client pro ; plus de
  facture par commande pour lui (le ticket reste), une facture récapitulative
  à la fin du mois.
- Réforme de la facture électronique : une PME émet ses factures entre
  entreprises via une plateforme agréée à partir du 1er septembre 2027 (à
  vérifier) ; un PDF ne suffira plus.

## Virement saisi depuis le back-office

- Versement reçu sur le compte bancaire, enregistré sans registre de caisse :
  dans la clôture fiscale et la comptabilité, pas dans un Z.
- Aujourd'hui un versement exige un registre ouvert sur la caisse.
- Droit envisagé : `customers.manage`.

## Abandon de créance depuis le back-office

- Même opération que depuis la caisse, sans registre, droit `pos.refund`.
