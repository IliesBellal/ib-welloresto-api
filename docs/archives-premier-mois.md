# Archives fiscales du premier mois (chaîne commencée en cours de mois)

## Contexte

Les clôtures journalières ont été mises en production le 9 octobre 2026 (le 15 pour l'établissement 242). Octobre est donc le premier mois dont la chaîne est incomplète : 23 clôtures journalières sur 31 (17 sur 31 pour le 242), mais une clôture mensuelle écrite le 31 octobre, somme des seuls jours clos.

`RunFiscalArchives` échouait chaque heure avec `period not fiscally closed: 23 jour(s) clos sur 31` : `checkClosed` exigeait une clôture par jour du mois. Ce n'est pas une régression, c'est la première fois qu'un mois est clôturé.

## Décisions

- **Aucune clôture n'est créée ni modifiée, aucun hash n'est réécrit.** Les 1er au 8 octobre n'ont jamais été clôturés ; leur total est repris dans la valeur d'ouverture de la clôture du 9 (`ouverture_json`). Les clôturer maintenant, ou recalculer la chaîne, reviendrait à réécrire un historique scellé (la vérification signale d'ailleurs tout parent `GENESIS_HASH` hors du premier maillon).
- **Archive mensuelle du premier mois : du premier jour clos à la fin du mois.** `Generate` (`KindMonth`) démarre à la première clôture journalière de l'établissement quand elle tombe dans le mois. La ligne `fiscal_archives` porte la vraie période (`period_start` = 2026-10-09), et le manifeste la rubrique `periode.premiere_cloture`. Les contrôles croisés (`Verify`) passent sans anomalie : tickets, clôtures journalières et clôture mensuelle couvrent la même période.
- **Idempotence par mois.** `PendingMonths` et `findMonth` reconnaissent l'archive d'un mois par `period_start` compris dans le mois (et non plus égal au 1er), sinon une archive débutant le 9 serait regénérée à chaque passage horaire.
- **Jours antérieurs à la première clôture : archive à la demande** (`KindPeriod`), acceptée uniquement si la période précède entièrement la première clôture. Une période à cheval reste refusée. Le manifeste porte `periode.anterieure_premiere_cloture = true` et `periode.premiere_cloture`, `Verify` n'y signale pas « tickets sans clôture journalière » (il n'y en a pas par construction), et `clotures.csv` est vide. La notice (section 4) l'explique.

## Mise en œuvre

- [internal/fiscalarchive/archive.go](../internal/fiscalarchive/archive.go) : `chainStart`, `Generate`, `checkClosed`, `findMonth`.
- [internal/fiscalarchive/due.go](../internal/fiscalarchive/due.go) : `PendingMonths`.
- [internal/fiscalarchive/build.go](../internal/fiscalarchive/build.go), [verify.go](../internal/fiscalarchive/verify.go), [notice.go](../internal/fiscalarchive/notice.go) : rubriques du manifeste, contrôle croisé, notice.
- [internal/fiscalarchive/prechain.go](../internal/fiscalarchive/prechain.go) et [cmd/generate_prechain_archives](../cmd/generate_prechain_archives/main.go) : génération en lot des archives antérieures à la première clôture.
- Test d'intégration : [archive_firstmonth_postgres_integration_test.go](../internal/fiscalarchive/archive_firstmonth_postgres_integration_test.go) (tag `postgres_integration`, à lancer sur une base de test avec `POSTGRES_URL` et `FISCAL_SIGNING_KEY`).

## Déploiement

1. Déployer l'API : au premier passage horaire, l'archive mensuelle d'octobre (du 9 au 31, du 15 au 31 pour le 242) est générée pour chaque établissement.
2. Simulation : `go run ./cmd/generate_prechain_archives --from=2026-10-01` (liste, n'écrit rien).
3. Pour de bon : mêmes variables que l'API (`POSTGRES_URL`, `FISCAL_SIGNING_KEY`, `R2_*` du bucket privé) avec `--apply`. Sûr à relancer.
4. Contrôle : `cmd/verify_fiscal` sur les établissements concernés.

## Points reportés

- Je n'ai pas vérifié que les chaînes des tickets, du journal et des caisses existaient déjà au 1er octobre : à contrôler en base avant de présenter l'archive du 1er au 8 comme justificatif.
- Le caractère suffisant de cette archive au regard de l'attestation de conformité est à confirmer par Ilies.
