# Version de l'appli par appareil (tablettes POS)

Permet de savoir sur quelle version de l'appli tourne chaque tablette POS d'un établissement.
Les bornes (`kiosks.app_version`) et les écrans cuisine (`cds_displays.app_version`) le faisaient
déjà via leur heartbeat ; les tablettes POS n'avaient aucune trace côté serveur.

## Fonctionnement

1. L'appli POS appelle déjà `POST /device/token` (démarrage de l'appli et rotation du token FCM).
   Elle y ajoute désormais `app_version` (pubspec `1.7.6`) et `version_code` (build `135`),
   lus via `package_info_plus`. Aucune requête supplémentaire.
2. `AuthService.SaveDeviceToken` enregistre le token FCM comme avant, **puis** écrit la version
   dans `users_devices` (`app_version`, `version_code`, `app_version_at`).
3. `GET /device/versions` (permission `settings.manage`) liste les appareils du merchant du token,
   les plus récemment utilisés d'abord, avec leur version.

## Décisions

- **Écriture de la version isolée et non bloquante.** C'est un `UPDATE` séparé de l'upsert du
  token FCM : s'il échoue (colonnes absentes avant la migration, par exemple), l'erreur est
  journalisée en `Warn` et l'enregistrement du token FCM — donc les notifications push — n'est pas
  affecté.
- **Champs facultatifs.** Une appli plus ancienne n'envoie rien : la ligne reste avec des versions
  `NULL` (`app_version_at` NULL = « jamais remontée »). Un `version_code` absent conserve la valeur
  connue (`COALESCE`).
- **Pas de version dans le check de mise à jour.** `POST /app/version/check` n'a pas de
  `device_id` et n'est appelé que depuis l'écran « Autres paramètres » ; ce n'est pas le bon point
  d'écriture. À noter : l'appli y envoie en dur `100` au lieu du vrai build
  (`authentication_controller.dart`, `checkAppVersion`) — comportement préexistant, non modifié ici.
- `app_version` tronqué à 20 caractères (comme `kiosks.app_version`).

## Déploiement

Migration `184_users_devices_app_version` (à appliquer manuellement) **avant** le code API.
Colonnes nullables sans défaut : pas de réécriture de table.

## Reste à faire

- Page back-office listant les appareils POS avec leur version (consommer `GET /device/versions`).
- Marquer comme « périmé » un appareil dont `version_code` est inférieur à la dernière
  `app_version` publiée pour `app_id` (comparaison à écrire côté back-office ou dans l'endpoint).
- Un appareil dont l'appli n'a pas été relancée depuis longtemps affiche sa version au dernier
  démarrage (`app_version_at`), pas en temps réel.
