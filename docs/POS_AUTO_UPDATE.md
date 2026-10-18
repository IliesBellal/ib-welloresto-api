# Mise à jour automatique du POS — côté API et publication

Date : 2026-10-17. Côté appli (service, installeur Android, réglages) :
`docs/POS_AUTO_UPDATE.md` du dépôt `wello_resto_flutter`.

## Changement d'API

`POST /app/version/check` (inchangé pour les anciens clients) renvoie en plus, quand
`status = update_available` :

| Clé | Contenu |
|---|---|
| `version_code` | numéro de build proposé (`app_version.version_code`) |
| `checksum_sha256` | empreinte de l'APK (`app_version.checksum_sha256`), **omise** si non renseignée |

Aucune migration : la colonne `checksum_sha256` existe depuis la migration 155 (vérifiée sur
staging). Sans empreinte, l'appli signale la version mais refuse de l'installer seule.

## Publier une version POS

1. Incrémenter le build dans `pubspec.yaml` (`version: 1.7.7+136` → `version_code = 136`).
2. Construire l'APK **signé avec la même clé que les tablettes** (voir l'avertissement sur la
   signature dans la doc de l'appli).
3. Calculer l'empreinte et téléverser l'APK :
   ```powershell
   Get-FileHash -Algorithm SHA256 app-release.apk   # → valeur en minuscules ou majuscules
   ```
4. Insérer la ligne (`app_id` du POS = `WR_RECEPTION`) :
   ```sql
   INSERT INTO app_version
     (app_id, version_code, last_functional_version_code, download_url, checksum_sha256, release_date)
   VALUES
     ('WR_RECEPTION', 136, 0,
      'https://storage.welloresto.fr/apps/reception/app-reception-136.apk',
      '<sha256 de l''APK>', '2026-10-20 02:00:00+02');
   ```
   - `release_date` : la version n'est proposée qu'après cette date (publier la veille au soir).
   - **Pilote** : ajouter des lignes `app_version_merchant (version_code, app_id, merchant_id)`
     pour ce `version_code` et `WR_RECEPTION` limite la version à ces établissements. Sans aucune
     ligne, elle est ouverte à tous.
   - Une empreinte erronée n'installe rien (rejet côté appli) : la corriger par `UPDATE`.

## Limite connue (préexistante)

`CheckAppVersion` ne regarde que la version la plus haute : si elle est restreinte à d'autres
établissements, un établissement non listé reçoit `no_update` même si une version plus basse,
ouverte à tous, existe. Sans conséquence tant qu'on publie dans l'ordre.
