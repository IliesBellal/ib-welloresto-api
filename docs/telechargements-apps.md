# Téléchargement public des applications et mise à jour de l'affichage client

Mis en place les 2026-11-08 et 2026-11-09. Modules : `internal/modules/appreleases` (téléchargement
public) et `internal/modules/cds` (mise à jour automatique de l'écran). Page :
`wello-resto-vitrine/src/pages/telechargements.astro` (journal : `docs/decisions-log.md` de la
vitrine, 2026-11-08). Écran : `wello-resto-customer-display-system`, `docs/CDS_DECISIONS.md` D22.

## Pourquoi

La page Téléchargements de la vitrine renvoyait vers trois pages PHP de `storage.welloresto.fr`
(`reception.php`, `kiosk.php`, `cds.php`, hébergées chez Hostinger), qui affichaient seulement un
bouton vers le dernier APK. `storage.welloresto.fr` doit passer sur Cloudflare R2, qui n'exécute pas
de PHP. La « dernière version » existe déjà en base : c'est la table `app_version`, que les
appareils consultent pour se mettre à jour. La vitrine passe donc par l'API.

## Téléchargement public : un seul endpoint

`GET /v1/public/apps/{app}/download` — public, sans authentification. Redirige (`302`, jamais mis
en cache) vers le `download_url` de la dernière version publique. C'est la cible directe du bouton
de la vitrine : un simple lien, sans JavaScript ni CORS. Le lien ne change pas d'une version à
l'autre : on peut le partager (e-mail d'installation, support).

Choix du fondateur (2026-11-09) : un seul endpoint. Une première version exposait aussi
`/latest` (JSON) pour afficher la date de la version sur la page ; retirée pour simplifier.

`{app}` est un nom public : `caisse` (app_id `WR_RECEPTION`), `borne` (`kiosk`), `cds` (`cds`).
Tout autre nom répond 404 sans interroger la base : les autres app_id (`WR_DELIVERY`, `WR_WAITER`,
app_id de test) ne sont jamais exposés. Sans version disponible, la réponse est un 404 en texte
lisible (le visiteur arrive par un lien), pas l'enveloppe JSON de l'API.

**Version proposée** : la plus haute `version_code` de l'application qui est à la fois

- **publiée** : `release_date` passée, comme pour les mises à jour des appareils ;
- **ouverte à tous** : aucune ligne dans `app_version_merchant` pour ce numéro et cet app_id. Une
  version en pilote n'est donc **pas** proposée au public ; la page propose la version ouverte
  précédente jusqu'à la fin du pilote (étape 8 de `docs/procedures/publier-version-pos.md`).

## Mise à jour automatique de l'affichage client

`POST /cds/app/version-check` (CDSAuth, token de l'écran), body `{ "version_code": <build
installé> }`. Même règle et même réponse que la borne (`POST /kiosk/app/version-check`) et le POS
(`POST /app/version/check`), sur `app_id = 'cds'` : plus haute version au-delà du build installé,
publiée ; une version en pilote n'est proposée qu'aux établissements listés. Écran révoqué :
refusé. L'écran télécharge, vérifie l'empreinte, et installe entre 04:00 et 05:00 quand aucune
commande n'est affichée (détails côté écran : `CDS_DECISIONS.md` D22).

### Publier une version de l'affichage client

Même procédure que la caisse (`docs/procedures/publier-version-pos.md`), avec ces différences :

1. Dans `pubspec.yaml` du dépôt `wello-resto-customer-display-system`, augmenter le numéro de build
   (après le `+`), strictement supérieur au dernier publié pour `app_id = 'cds'`.
2. Construire l'APK, contrôler la signature (même clé d'une version à l'autre : Android refuse
   sinon la mise à jour), renommer en `app-cds-<numéro de build>.apk`, calculer l'empreinte
   (`Get-FileHash -Algorithm SHA256`).
3. Téléverser sous `storage.welloresto.fr/apps/cds/`.
4. Déclarer la version :

   ```sql
   INSERT INTO app_version
     (app_id, version_code, last_functional_version_code, download_url, checksum_sha256, release_date)
   VALUES
     ('cds', <numéro de build>, 0,
      'https://storage.welloresto.fr/apps/cds/app-cds-<numéro de build>.apk',
      '<empreinte>', now());
   ```

   L'empreinte est **obligatoire** : sans elle, l'écran refuse la mise à jour.
5. Pilote possible exactement comme pour la caisse (`app_version_merchant`, `app_id = 'cds'`).

**Une seule fois** : la première version qui contient la mise à jour automatique s'installe à la
main sur chaque box. Au démarrage, l'écran demande alors l'autorisation « Installer des applis
inconnues » (à accorder à la télécommande). Les boxes sous Android 11 ou moins ne peuvent pas
s'installer une mise à jour sans confirmation : elles restent à mettre à jour à la main.

## Mise en ligne

1. Déployer l'API (`/v1/public/apps/{app}/download` et `/cds/app/version-check`).
2. Vérifier en prod que `/v1/public/apps/caisse/download` et `/borne/download` mènent au bon APK,
   et déclarer la version courante de l'affichage client (ci-dessus).
3. Déployer la vitrine.
4. Les pages PHP de `storage.welloresto.fr` ne sont plus liées par la vitrine. Elles peuvent
   disparaître à la bascule de `storage.welloresto.fr` vers R2 ; les APK, eux, y sont copiés à
   l'identique (mêmes chemins), donc les `download_url` existantes restent valables.

## Tests

- `appreleases/handler_test.go` : correspondance nom public → app_id, nom inconnu ou interne sans
  requête en base, redirection 302 sans cache, 404 lisible, 500.
- `appreleases/postgres_integration_test.go` et `cds/app_version_postgres_integration_test.go`
  (`-tags postgres_integration`, joués sur staging le 2026-11-08/09) : version pilote écartée
  (public) ou réservée (écran), version future écartée, filtre par app_id, build à jour.
