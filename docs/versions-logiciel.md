# Versions du logiciel de caisse WelloResto

Référence de la version attestée (BOI-TVA-DECLA-30-10-30 §340 et §375 ;
modèle d'attestation BOI-LETTRE-000242). Code : [internal/version](../internal/version/version.go),
route publique `GET /version`.

## Ce qui est attesté

Le **système de caisse WelloResto** : l'API, qui porte toutes les fonctions
fiscales, et les applications qui enregistrent les règlements des clients
(caisse, borne, ScanNOrder, plateformes de livraison). Les fonctions
fiscales sont toutes côté serveur :
- empreintes et chaînes signées ;
- clôtures journalières, mensuelles et annuelles ;
- archives ;
- contrôle d'intégrité.

La caisse imprime le ticket figé par l'API.

Le numéro de version est celui de l'**API**.

## Règle de numérotation

`MAJEUR.MINEUR.CORRECTIF`, racine majeure **2** (décision d'Ilies,
2026-10-08) :

- **2.1.6** : première version attestée, mise en production unique des lots A
  à F de la conformité caisse.
- **2.x.y** (subdivisions de la racine 2) : versions mineures et correctifs
  qui ne touchent pas aux conditions d'inaltérabilité, de sécurisation, de
  conservation et d'archivage. L'attestation en cours reste valable.
- **3.0.0** : la prochaine version qui modifie l'une de ces conditions,
  avec une **nouvelle attestation** pour chaque établissement. Une attestation
  dont la racine diffère de la version en service est signalée comme
  périmée au back-office et à la caisse.

**Garde-fou de revue.** Toute modification de l'un des éléments suivants
oblige à décider, dans la revue et dans la note de version ci-dessous, si
la version est majeure ou non :
- `internal/fiscal`, `internal/fiscalarchive`, `internal/fiscalverify` ;
- les migrations des tables scellées ;
- l'émission des tickets.

## Historique

| Version | Date de mise sur le marché | Nature | Note |
|---|---|---|---|
| 2.1.6 | à la mise en production | majeure | Lots A à F de la conformité caisse : chaînes v2 signées, clôtures fiscales, réouverture encadrée, archives, contrôle d'intégrité, ticket fiscal imprimé, attestation. |
