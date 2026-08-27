# Vérification email/téléphone retirée du RBAC (lot 2.5)

## Ce qui existait

`internal/middleware/permissions.go` exposait deux prédicats, `IsEmailVerified` et
`IsTelVerified`, factorisés dans `forbiddenCode` (`internal/middleware/require_permission.go`).
`RequirePermission` et `RequireAdmin` appelaient `forbiddenCode` pour choisir le code d'erreur
renvoyé **une fois l'accès déjà refusé** (permission manquante ou compte non admin) :

- `EMAIL_VERIFICATION_REQUIRED` si `!IsEmailVerified(user)` ;
- `TEL_VERIFICATION_REQUIRED` si `user.Rights.Admin && !IsTelVerified(user)` ;
- `access_denied` sinon.

`IsEmailVerified` retournait `true` inconditionnellement depuis longtemps (période de grâce
jamais désactivée — voir le commentaire `TODO` qu'elle portait), donc en pratique seule la
branche `IsTelVerified` avait un effet observable, et seulement sur le *libellé* de l'erreur, pas
sur la décision d'accès elle-même (celle-ci était déjà prise par `RequirePermission`/`RequireAdmin`
avant d'appeler `forbiddenCode`).

## Ce qui a été retiré

- `IsTelVerified` et `IsEmailVerified` (`internal/middleware/permissions.go`).
- `forbiddenCode` et les deux branches conditionnelles qu'elle contenait
  (`internal/middleware/require_permission.go`). `RequirePermission` et `RequireAdmin` renvoient
  désormais directement `"access_denied"` sur un refus, quel que soit le motif.
- Aucun autre point du code ne dépendait de `IsFullyVerified` (déjà absent avant ce lot) ni des
  codes `EMAIL_VERIFICATION_REQUIRED` / `TEL_VERIFICATION_REQUIRED`.

## Pourquoi ce n'est pas un droit

Le statut de vérification email/téléphone décrit **l'état d'un établissement** (son responsable
a-t-il confirmé ses coordonnées ?), pas une autorisation accordée à l'utilisateur qui fait la
requête. Le confondre avec RBAC posait deux problèmes :

1. **Mauvais sujet vérifié.** `IsTelVerified`/`IsEmailVerified` lisaient
   `user.TelVerifiedAt`/`user.EmailVerifiedAt` — ceux de l'utilisateur **connecté**, pas ceux du
   responsable de l'établissement. Un employé avec un compte jamais vérifié aurait pu se voir
   opposer un refus dont le message n'avait aucun rapport avec l'état réel de l'établissement.
2. **Mauvaise couche.** RBAC répond à « cet utilisateur a-t-il ce droit ? ». La vérification
   répond à « cet établissement a-t-il confirmé qui le dirige ? ». Superposer les deux dans
   `forbiddenCode` masquait la vraie nature de la contrainte et l'accrochait à un mécanisme
   (permission.Key, rôles) qui n'est pas fait pour porter un état d'établissement.

## Ce qui reste intact

Aucune donnée n'a été touchée : les colonnes `users.email_verified_at`, `users.tel_verified_at`,
`users.mfa_*`, et les flux qui les alimentent (envoi de code de vérification, confirmation) sont
inchangés. Seule la décision d'autorisation qui s'appuyait dessus a disparu.

## Ce qu'il reste à concevoir

Un lot séparé doit redéfinir la règle « email et téléphone du responsable vérifiés pour passer des
commandes » comme un état d'établissement, pas un droit utilisateur. Reste à trancher :

- **Quel sujet est vérifié ?** Le responsable légal de l'établissement (`merchant`), pas
  l'utilisateur qui envoie la requête — à définir précisément (quel rôle/quelle relation porte
  cette identité sur `merchant`).
- **Quelles écritures sont concernées ?** L'ancien code ne gardait que le sous-groupe d'écriture
  de `/orders` (retiré en RBAC lot 2, avant ce lot — voir `docs/RBAC_ROUTES.md`, section
  `/orders`) — périmètre à revalider : commandes seules, ou aussi paiements, remboursements,
  autres écritures sensibles ?
- **Où vit la décision ?** Probablement un contrôle au niveau du service `orders` (ou un
  middleware dédié à l'état de l'établissement), distinct de `RequirePermission`/`RequireAdmin`,
  pour ne pas répéter la confusion sujet/couche décrite ci-dessus.

Voir aussi `docs/RBAC_ROUTES.md` pour l'inventaire des routes et leur garde actuelle.
