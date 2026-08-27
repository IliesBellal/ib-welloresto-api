package middleware

import (
	"welloresto-api/internal/modules/auth"
)

// ============================================================
// RBAC lot 2 — bascule des prédicats
//
// Toutes les fonctions HasXxx/CanXxx ainsi que les combinateurs AnyOf/AllOf
// ont été retirées : RequirePermission prend désormais directement une
// permission.Key et appelle user.Has(key), qui encapsule la correspondance
// avec les anciennes colonnes booléennes (voir internal/modules/auth/
// permissions.go). Elles ont été supprimées plutôt que dépréciées car aucune
// n'avait plus d'appelant réel dans cmd/api/routes.go au moment de la bascule
// (vérifié : seules HasMenuAccess, HasPlanningAccess, HasUserManagementAccess,
// HasSettingsAccess, HasHACCPAccess, HasCustomerManagementAccess et IsAdmin
// étaient effectivement câblées sur une route).
//
// IsAdmin reste à part : il correspond à « détient tous les droits », pas à
// un droit particulier du catalogue — voir middleware.RequireAdmin.
//
// RBAC lot 2.5 : IsEmailVerified et IsTelVerified (et leur factorisation dans
// require_permission.go's forbiddenCode) ont été retirées. Ce n'étaient pas
// des droits RBAC mais un statut de vérification de compte détourné en
// décision d'autorisation — et qui vérifiait de toute façon l'utilisateur
// connecté plutôt que le responsable de l'établissement. Voir
// docs/RBAC_VERIFICATION_RETIREE.md pour le détail de ce qui a été retiré et
// ce qu'il reste à concevoir. Les colonnes users.email_verified_at /
// tel_verified_at et les flux qui les alimentent restent intacts — seule la
// décision d'autorisation a disparu.
// ============================================================

// IsAdmin vérifie que l'utilisateur est administrateur
func IsAdmin(user *auth.UserLoginRow) bool {
	return user.IsAdmin()
}
