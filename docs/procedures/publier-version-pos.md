# Publier une nouvelle version du POS (distribution par APK)

Procédure à suivre à chaque nouvelle version de la caisse (`wello_resto_flutter`). Fonctionnement
de la mise à jour automatique : `docs/POS_AUTO_UPDATE.md` (ce dépôt et celui de l'appli). Création
de la clé de signature : `docs/POS_AUTO_UPDATE.md` du dépôt de l'appli, « Clé de signature ».

> Valable tant que la distribution se fait par APK. Au passage en store, l'auto-mise à jour est
> retirée et cette procédure est remplacée (voir la checklist « Passage en store » de la doc de
> l'appli).

## Une seule fois, avant la première version avec auto-mise à jour

1. Appliquer la **migration 184** (versions des appareils), puis **déployer l'API**. Elle renvoie
   alors l'empreinte et le numéro de version, et `GET /device/versions` fonctionne.
2. **Installer à la main sur chaque tablette** cette première version : les versions actuelles ne
   savent pas se mettre à jour.
3. Sur chaque tablette, ouvrir la ligne de version dans les réglages et accorder « Installer des
   applis inconnues » (la popup d'autorisation guide).

## À chaque nouvelle version

### 1. Préparer

Dans `pubspec.yaml`, augmenter la version : `1.7.6+135` devient par exemple `1.7.7+136`. Le chiffre
après le `+` est le numéro de build (`version_code`). Il doit être **strictement supérieur** à tous
ceux déjà publiés et ne se réutilise jamais. Vérifier le plus haut `version_code` de
`app_version` **en prod** (staging n'est pas représentatif : 129 pour un projet à 135).

### 2. Construire et contrôler la signature

```powershell
flutter build apk --release
$env:JAVA_HOME = "C:\Program Files\Android\Android Studio1\jbr"
& "D:\Android\Sdk\build-tools\36.0.0\apksigner.bat" verify --print-certs build\app\outputs\flutter-apk\app-release.apk
```

Le `SHA-256 digest` doit être exactement
`6f568c32fb5ceec833b2ba46e2b9133ee3d4e4dfb427f37a6b9a47b9d57afef4`. **S'il diffère, ne pas publier.**

### 3. Renommer et calculer l'empreinte

Renommer en `app-reception-<numéro de build>.apk` (ex. `app-reception-136.apk`), puis :

```powershell
Get-FileHash -Algorithm SHA256 .\app-reception-136.apk
```

Garder la valeur affichée.

### 4. Téléverser

Sur `storage.welloresto.fr/apps/reception/`, comme aujourd'hui (la méthode de téléversement n'est
décrite dans aucun dépôt). Vérifier ensuite que le lien se télécharge et que la taille du fichier
est correcte.

### 5. Déclarer la version en base de prod

À exécuter par la personne qui gère la base :

```sql
INSERT INTO app_version
  (app_id, version_code, last_functional_version_code, download_url, checksum_sha256, release_date)
VALUES
  ('WR_RECEPTION', 136, 0,
   'https://storage.welloresto.fr/apps/reception/app-reception-136.apk',
   '<empreinte de l''étape 3>', '2026-10-20 02:00:00+02');
```

`release_date` : la version n'est proposée qu'après cette date.

### 6. Pilote d'abord

Limiter la version à un ou deux établissements de test :

```sql
INSERT INTO app_version_merchant (version_code, app_id, merchant_id)
VALUES (136, 'WR_RECEPTION', '<id établissement test>');
```

Dès qu'une ligne existe pour ce numéro de version, seuls les établissements listés la reçoivent.

### 7. Vérifier sur une tablette pilote

- Réglages › ligne de version › **Rechercher** : téléchargement, puis « Nouvelle version prête ».
- **Installer maintenant** pour ne pas attendre la fenêtre de 04:00. La première fois, Android
  demande une confirmation.
- Après relance, `GET /device/versions` affiche le nouveau numéro de build pour cette tablette.

### 8. Ouvrir à tous

```sql
DELETE FROM app_version_merchant WHERE version_code = 136 AND app_id = 'WR_RECEPTION';
```

Sans aucune ligne pour ce numéro, la version est ouverte à tous. Les tablettes la téléchargent
dans l'heure et l'installent à 04:00, si l'écran n'a pas été touché depuis 5 minutes et qu'aucun
paiement n'est en attente.

## Si une version pose problème

- **On ne revient pas en arrière** : Android refuse un numéro de build plus bas. Il faut publier une
  version suivante (137) qui corrige.
- **Arrêter la diffusion immédiatement** : supprimer la ligne de `app_version`, ou la restreindre à
  un établissement de test. Les tablettes qui l'avaient téléchargée sans l'installer l'effacent à
  leur prochain contrôle ; celles qui l'ont installée la gardent.

## Règles à ne jamais enfreindre

- Ne jamais réutiliser un numéro de build.
- Ne jamais remplacer un APK déjà publié par un autre fichier sous le même nom : l'empreinte ne
  correspondrait plus et les tablettes le rejetteraient.
- Garder les anciens APK sur le stockage.
- Ne jamais publier un APK dont la signature diffère de `6f568c32…afef4`.

## Limites à connaître

- Une tablette ne se met à jour que si l'appli est ouverte, connectée à un compte et sur le Wi-Fi à
  l'heure de la fenêtre. Sinon elle réessaie la nuit suivante.
- La version affichée dans `GET /device/versions` est celle du dernier démarrage de la tablette.
- Pas encore testé sur une vraie tablette : la première version déployée sert de recette, d'où le
  pilote (étape 6).
