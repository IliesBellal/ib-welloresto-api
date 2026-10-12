# Export PDF des registres de caisse

Depuis la liste des registres du back-office, on télécharge le PDF d'un ou de
plusieurs registres (sélection par cases à cocher, ou toute la période
affichée). Un registre donne un PDF ; plusieurs donnent un ZIP, un PDF par
registre.

## Ce que contient le PDF d'un registre

C'est le **rapport comptable mensuel restreint à un registre** : mêmes sections
(TVA, remises, encaissements), mêmes règles de calcul, selon le **mode de
clôture du registre** (`cash_registers.closing_mode`, voir
[EXPORT_COMPTABLE_MODES_CLOTURE.md](EXPORT_COMPTABLE_MODES_CLOTURE.md)) :

| | MANUAL | AUTO |
|---|---|---|
| TVA | sur les lignes, remises déduites (`buildManualVAT`) | ventilée à partir des paiements (`buildAutoReport`) |
| Encaissements | **réel** : relevé de caisse (`cash_registers_custom_items`) | **théorique** : paiements enregistrés (pas de relevé) |
| Écart | non affiché | sans objet |

Le tableau « Théorique / Réel / Écart » de l'ancien PDF est supprimé : le réel
d'un registre MANUAL est identique à celui du rapport mensuel, et la clôture
exige un écart proche de 0. L'en-tête indique le mode de clôture.

Un registre doit être **validé** (`enclosed`) : le PDF s'appuie sur des
données figées. En AUTO, la validation suit immédiatement la fermeture
(`CloseCashRegister`, `service.go`) ; aucune tâche ne ferme de registre, donc
un registre AUTO jamais fermé reste hors export.

## API

`POST /accounting/registers/export-pdf` (permission `reports.financial.read`,
comme l'export unitaire existant `POST /accounting/registers/{id}/export-pdf`).

Corps : soit `{"register_ids": ["12", "13"]}`, soit
`{"date_from": "2026-09-01", "date_to": "2026-09-30"}`, jamais les deux.

- **Plafond : 100 registres** (`RegisterExportMax`), génération synchrone.
- **Période = la liste affichée.** L'API réutilise la requête de l'historique
  (`GetCashRegisterHistory`) : mêmes bornes sur `start_date` (une date seule
  couvre la journée entière, une date-heure est reprise telle quelle) et même
  visibilité (registres de l'utilisateur, tous pour un administrateur). Le
  total de la période (registres non validés compris) est comparé au plafond.
- Réponse : `application/zip`, en-têtes `X-Registers-Exported` et
  `X-Registers-Ignored` (exposés par le CORS). Fichiers :
  `WR_registre_<id>_<AAAA-MM-JJ>.pdf`, plus `ignores.txt` si des registres sont
  écartés (non validés, introuvables, erreur de génération), avec la raison.
- Un registre écarté ne fait pas échouer le lot. Rien d'exportable : 409
  `register_export_none_eligible` ; erreur technique sur tous : l'erreur est
  rendue telle quelle (500).
- Erreurs (message français au premier niveau de la réponse) :
  `register_export_empty` (400), `register_export_too_many` (400),
  `register_not_enclosed` (409, export unitaire), `not_found` (404, registre
  inconnu ou d'un autre établissement : jamais son contenu).
- L'export unitaire `POST /accounting/registers/{id}/export-pdf` est inchangé
  côté contrat ; seul le contenu du PDF change.

## Back-office

Page « Registres de caisse » ([CashRegisterHistory.tsx](../../wello-back-office/src/pages/CashRegisterHistory.tsx)) :

- une case par ligne (grisée, avec info-bulle, si le registre n'est pas validé),
  une case d'en-tête qui coche les lignes exportables **de la page** ;
- quand toute la page est cochée et que la période compte d'autres pages, un
  bandeau propose « Sélectionner les N registres de la période » : l'export part
  alors avec les dates de la liste et non avec des identifiants ;
- barre d'action « Télécharger (N) », désactivée au-delà de 100 ;
- un seul registre sélectionné : son PDF directement ; plusieurs : un ZIP.

`exportPeriodRegisters` (appelait une route `export-period` qui n'a jamais
existé dans l'API, et que personne n'utilisait) est supprimée au profit de
`exportRegistersPDF`.

`ExpandableDataTable` gagne une prop optionnelle `selection` (cases à cocher,
état tenu par le parent) ; sans elle, rien ne change pour les autres tableaux.

## Décisions

- **ZIP, un PDF par registre** plutôt qu'un PDF fusionné : chaque document reste
  autonome pour un comptable ou un contrôle, et la génération réutilise celle de
  l'export unitaire.
- **Synchrone, plafond 100.** 62 registres par mois avec 2 caisses, plus des
  clôtures midi/soir : 50 était trop juste. Au-delà, il faudra passer par le
  mécanisme des exports archivés (R2 + lien signé).
- **Période = requête de l'historique**, pas les jours de calendrier de
  l'établissement du rapport mensuel : « tout sélectionner » doit donner exactement
  ce que la liste montre, aux bornes de jour comme pour la visibilité.
- **Réel en MANUAL, théorique en AUTO**, sans colonne écart (décision Ilies).
- **Aucune trace d'audit** : comme l'export unitaire et l'export comptable, ces
  PDF sont une lecture de données déjà scellées ; seule l'archive fiscale
  (`FISCAL_ARCHIVE_DOWNLOAD`) écrit dans la chaîne d'audit.
- **Registre « en dérive »** (MANUAL : le recalcul des paiements ne correspond
  plus à l'instantané figé, `IsRegisterTrusted`) : le PDF est produit avec la
  mention « modifié après sa clôture… écarté du rapport mensuel », au lieu d'être
  refusé. Les verrous du lot C (paiements d'un registre fermé non modifiables)
  doivent rendre ce cas impossible pour les nouvelles données ; il ne peut venir
  que de données antérieures. Le contrôle reste un filet de sécurité. En AUTO, TVA
  et encaissements sont tous deux recalculés en direct : pas de dérive possible.

## Limites connues

- Le rapprochement « somme des PDF de registres = rapport mensuel » n'est pas
  garanti à l'euro : le mensuel découpe par date de création des commandes,
  le registre par les commandes qui lui sont rattachées (une commande payée sur un
  autre registre ou à cheval sur minuit peut changer de côté). Non mesuré sur des
  données réelles.
- Durée d'un lot de 100 PDF non mesurée ; le délai d'écriture du serveur et celui
  du proxy n'ont pas été vérifiés.
- Le test d'intégration Postgres (`register_pdf_postgres_integration_test.go`,
  tag `postgres_integration`) compile mais n'a pas été exécuté : pas de
  `POSTGRES_URL` en local. Les tests unitaires (validation de la demande, contenu
  du PDF par mode, mention de dérive, périmètre SQL) passent.

Les commandes de borne (sentinelle `'KIOSK'` dans `orders.cash_register_id`) suivent
les paiements du registre pour la TVA — voir [ECART_REGISTRES_BORNE.md](ECART_REGISTRES_BORNE.md).

## Fichiers

- API : [register_pdf.go](../internal/modules/pos/accounting/register_pdf.go)
  (PDF, ZIP, résolution de la demande), [handler.go](../internal/modules/pos/accounting/handler.go),
  [repository.go](../internal/modules/pos/accounting/repository.go)
  (`orderScope.registerID`, `IsRegisterTrusted`), `models/responses_models.go`
  (erreurs), `middleware/cors.go` (en-têtes exposés), `cmd/api/routes.go` (route).
- Back-office : `pages/CashRegisterHistory.tsx`,
  `services/cashRegisterHistoryService.ts`, `components/shared/ExpandableDataTable.tsx`.
- Tests : `register_pdf_test.go`, `register_pdf_postgres_integration_test.go`.
