# Admin Users And Employees API

Cette API complète la page backoffice "Employés" avec trois notions distinctes :

- `user` : compte global partagé au niveau plateforme.
- `users_rights` : droits merchant-scoped d'un `user` pour un établissement donné.
- `employee` : fiche planning merchant-scoped, liée ou non à un `user`.

## Endpoints admin users

- `GET /users`
  - Filtres : `search`, `active`, `linked_employee`, `admin`, `page`, `page_size`
  - Retourne la liste merchant-scoped des comptes liés avec photo, identité, création, `last_login_at`, statut, droits et fiche employé éventuelle.
- `POST /users`
  - Crée un compte global puis le lie au merchant courant.
  - Payload principal : `first_name`, `last_name`, `email`, `password`, `tel`, `rights`.
- `GET /users/{id}`
  - Retourne le détail admin complet pour le merchant courant.
- `POST /users/link-by-email`
  - Lie au merchant courant le compte actif portant cet e-mail, **s'il existe**, et envoie à son titulaire le mail `merchant_user_linked.html` (« Votre compte a été rattaché à … », expéditeur sécurité).
  - Payload : `email`, `rights` (optionnel, comme `merchant-link`), `admin`.
  - **Réponse anonyme** : `202` avec le même message (« Si un compte existe avec cette adresse… ») que le compte existe, soit déjà lié, soit introuvable, soit en double. Seules une adresse invalide (`400`) et une erreur technique sont signalées. C'est voulu : le back-office ne doit pas permettre de savoir si une adresse a un compte Wello Resto — seul son titulaire l'apprend, par le mail. Ne jamais ajouter d'information sur le compte à cette réponse.
  - Deux comptes actifs pour la même adresse : rien n'est lié (on ne devine pas lequel).
  - Le mail est envoyé de façon asynchrone (échec d'envoi journalisé, sans effet sur le lien).
- `GET /users/linkable-search?search=`
  - Recherche des comptes globaux non encore liés au merchant courant. **N'est plus utilisé par le formulaire « Lier un existant »** (remplacé par `link-by-email`) ; ne reste appelé que par le panneau « lier à une fiche employé » de `EmployeesModal`. Expose nom, e-mail et téléphone de comptes d'autres établissements : à retirer (voir « Reporté » ci-dessous).
- `POST /users/{id}/merchant-link`
  - Lie un compte global existant au merchant courant, par identifiant.
  - Payload : `rights`.
  - Envoie aussi le mail « compte rattaché » au titulaire (best-effort).
  - Répond `404`/`409` selon l'existence / le lien : plus utilisé par le back-office, à réserver à un usage où l'identifiant est déjà connu.
- `DELETE /users/{id}/merchant-link`
  - Délie le compte du merchant courant et enlève aussi les liaisons `employees.user_id` du merchant.
- `GET /users/{id}/rights`
  - Retourne le modèle complet des droits merchant-scoped.
- `PUT /users/{id}/rights`
  - Met à jour les droits merchant-scoped.
  - Payload :

```json
{
  "admin": false,
  "permissions": {
    "access_reception": true,
    "access_delivery": false,
    "access_waiter": false,
    "print_merchant_cash_report": false,
    "open_cash_drawer": false,
    "manage_menu": true,
    "manage_plannings": true,
    "manage_users": true,
    "manage_settings": false,
    "manage_haccp": false,
    "view_reports": true,
    "export_reports": false,
    "view_financials": false,
    "export_financials": false,
    "manage_customers": false,
    "export_customers": false
  }
}
```

- `POST /users/{id}/force-reset-password`
  - Réservé admin.
  - Payload : `{ "new_password": "..." }`
  - Met à jour le mot de passe et invalide les tokens actifs.

## Endpoints planning employés

- `GET /planning/employees`
  - Filtres supplémentaires : `user_id`, `unlinked`.
- `POST /planning/employees/{id}/user-link`
  - Payload : `{ "user_id": "usr_..." }`
  - Valide que le user est bien lié au merchant courant et qu'il n'existe pas déjà un autre employee actif pour ce user.
- `DELETE /planning/employees/{id}/user-link`
  - Délie explicitement `employee.user_id`.

## Sécurité

- Les endpoints admin users exigent `HasUserManagementAccess`.
- `force-reset-password` et `DELETE /users/{id}/merchant-link` exigent `IsAdmin`.
- Les endpoints planning admin exigent `HasPlanningAccess`.

## Dernière connexion

- `users.last_login_at` est mis à jour après un login réussi.
- En cas de MFA backoffice, la date est mise à jour après validation MFA réussie.
## Reporté — liaison par e-mail (2026-11-08)

- **Retirer `GET /users/linkable-search`** : il permet à tout détenteur de `staff.manage` de lister nom/e-mail/téléphone de comptes sans lien avec son établissement, ce que `link-by-email` évite. Bloqué par `EmployeesModal.LinkAccountPanel` (back-office), qui l'utilise pour « lier à une fiche employé » — or `validateEmployeeUserLink` exige que le compte soit **déjà** lié au merchant, donc ce panneau ne peut aboutir que pour des comptes que la recherche exclut. À traiter avec la refonte de ce panneau (choisir parmi les membres déjà liés).
- **`POST /users/{id}/merchant-link`** : avec des identifiants entiers (chantier ids entiers), le `404` vs `409` permet de sonder les comptes par identifiant. Le retirer ou l'aligner sur la réponse anonyme quand plus aucun client ne l'appelle.
- Le mail n'a pas de bouton de connexion (aucune URL de back-office en configuration côté API) ; à ajouter si une variable dédiée est créée.
