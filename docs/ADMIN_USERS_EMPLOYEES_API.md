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
- `GET /users/linkable-search?search=`
  - Recherche des comptes globaux non encore liés au merchant courant.
- `POST /users/{id}/merchant-link`
  - Lie un compte global existant au merchant courant.
  - Payload : `rights`.
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