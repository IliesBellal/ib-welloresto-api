# Téléchargement public des applications (page Téléchargements du site vitrine)

Mis en place le 2026-11-08. Module : `internal/modules/appreleases`. Page :
`wello-resto-vitrine/src/pages/telechargements.astro` (journal : `docs/decisions-log.md` de la
vitrine, même date).

## Pourquoi

La page Téléchargements de la vitrine renvoyait vers trois pages PHP de `storage.welloresto.fr`
(`reception.php`, `kiosk.php`, `cds.php`, hébergées chez Hostinger), qui affichaient seulement un
bouton vers le dernier APK. `storage.welloresto.fr` doit passer sur Cloudflare R2, qui n'exécute pas
de PHP. La « dernière version » existe déjà en base : c'est la table `app_version`, que les
appareils consultent pour se mettre à jour. La vitrine la lit donc directement via l'API.

## Routes (publiques, sans authentification)

| Route | Réponse |
|---|---|
| `GET /v1/public/apps/{app}/latest` | `{ id, data: { app, version_code, release_date, download_url } }`, mis en cache 5 min |
| `GET /v1/public/apps/{app}/download` | `302` vers `download_url`, jamais mis en cache — cible du bouton de la vitrine |

`{app}` est un nom public : `caisse` (app_id `WR_RECEPTION`), `borne` (`kiosk`), `cds` (`cds`).
Tout autre nom répond 404 sans interroger la base : les autres app_id (`WR_DELIVERY`, `WR_WAITER`,
app_id de test) ne sont jamais exposés.

Sans version disponible, `/download` répond un 404 en texte lisible (le visiteur arrive par un
lien), et la vitrine remplace le bouton par « Bientôt disponible ». Les deux routes sont appelées
depuis `welloresto.fr` / `www.welloresto.fr`, déjà dans la liste CORS ; `/download` n'a de toute
façon pas besoin du CORS (simple lien).

## Quelle version est proposée

La plus haute `version_code` de l'application qui est à la fois :

- **publiée** : `release_date` passée, comme pour les mises à jour des appareils ;
- **ouverte à tous** : aucune ligne dans `app_version_merchant` pour ce numéro et cet app_id. Une
  version en pilote n'est donc **pas** proposée au public ; la page affiche la version ouverte
  précédente jusqu'à la fin du pilote (étape 8 de `docs/procedures/publier-version-pos.md`).

Le lien `/v1/public/apps/caisse/download` reste le même d'une version à l'autre : on peut le
partager (e-mail d'installation, support).

## Affichage client (CDS)

L'affichage client ne se met pas à jour tout seul : jusqu'ici, aucune de ses versions n'était
déclarée dans `app_version`. Pour que sa carte propose un téléchargement, déclarer chaque version
sous `app_id = 'cds'` (aucun appareil ne lit ces lignes, elles ne servent qu'à cette page) :

```sql
INSERT INTO app_version
  (app_id, version_code, last_functional_version_code, download_url, checksum_sha256, release_date)
VALUES
  ('cds', <numéro de build>, 0, 'https://storage.welloresto.fr/apps/cds/<fichier>.apk', '<sha256>', now());
```

## Mise en ligne

1. Déployer l'API (routes ci-dessus).
2. Vérifier en prod que `GET /v1/public/apps/caisse/latest` et `/borne/latest` renvoient la bonne
   version, et déclarer la version courante de l'affichage client (ci-dessus).
3. Déployer la vitrine.
4. Les pages PHP de `storage.welloresto.fr` ne sont plus liées par la vitrine. Elles peuvent
   disparaître à la bascule de `storage.welloresto.fr` vers R2 ; les APK, eux, y sont copiés à
   l'identique (mêmes chemins), donc les `download_url` existantes restent valables.

## Tests

- `handler_test.go` : correspondance nom public → app_id, nom inconnu ou interne sans requête en
  base, réponse JSON et cache de `/latest`, redirection 302 sans cache, 404 lisible, 500.
- `postgres_integration_test.go` (`-tags postgres_integration`, joué sur staging le 2026-11-08) :
  version pilote écartée, version future écartée, filtre par app_id, version pilote proposée après
  la fin du pilote.
