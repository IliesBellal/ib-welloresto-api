package middleware

import "welloresto-api/internal/modules/auth"

// ============================================================
// PERMISSION HELPERS
// Ces fonctions sont des PermissionFunc prêtes à l'emploi
// Utilisation : middleware.RequirePermission(middleware.IsAdmin)
// ============================================================

// IsAdmin vérifie que l'utilisateur est administrateur
func IsAdmin(user *auth.UserLoginRow) bool {
	return user.IsAdmin()
}

// HasAccessReception vérifie que l'utilisateur a accès à la réception
func HasAccessReception(user *auth.UserLoginRow) bool {
	return user.HasAccessReception()
}

// HasAccessDelivery vérifie que l'utilisateur a accès à la livraison
func HasAccessDelivery(user *auth.UserLoginRow) bool {
	return user.HasAccessDelivery()
}

// HasAccessWaiter vérifie que l'utilisateur a accès au module serveur
func HasAccessWaiter(user *auth.UserLoginRow) bool {
	return user.HasAccessWaiter()
}

// CanPrintCashReport vérifie que l'utilisateur peut imprimer les rapports de caisse
func CanPrintCashReport(user *auth.UserLoginRow) bool {
	return user.CanPrintCashReport()
}

// CanOpenCashDrawer vérifie que l'utilisateur peut ouvrir le tiroir-caisse
func CanOpenCashDrawer(user *auth.UserLoginRow) bool {
	return user.CanOpenCashDrawer()
}

// HasMenuAccess vérifie que l'utilisateur peut gérer le menu
func HasMenuAccess(user *auth.UserLoginRow) bool {
	return user.HasMenuAccess()
}

// HasPlanningAccess vérifie que l'utilisateur peut gérer les plannings
func HasPlanningAccess(user *auth.UserLoginRow) bool {
	return user.HasPlanningAccess()
}

// HasUserManagementAccess vérifie que l'utilisateur peut gérer les utilisateurs
func HasUserManagementAccess(user *auth.UserLoginRow) bool {
	return user.HasUserManagementAccess()
}

// HasSettingsAccess vérifie que l'utilisateur peut gérer les paramètres
func HasSettingsAccess(user *auth.UserLoginRow) bool {
	return user.HasSettingsAccess()
}

// HasHACCPAccess vérifie que l'utilisateur peut gérer le HACCP
func HasHACCPAccess(user *auth.UserLoginRow) bool {
	return user.HasHACCPAccess()
}

// HasReportsViewAccess vérifie que l'utilisateur peut consulter les rapports
func HasReportsViewAccess(user *auth.UserLoginRow) bool {
	return user.HasReportsViewAccess()
}

// HasReportsExportAccess vérifie que l'utilisateur peut exporter les rapports
func HasReportsExportAccess(user *auth.UserLoginRow) bool {
	return user.HasReportsExportAccess()
}

// HasFinancialsViewAccess vérifie que l'utilisateur peut consulter les données financières
func HasFinancialsViewAccess(user *auth.UserLoginRow) bool {
	return user.HasFinancialsViewAccess()
}

// HasFinancialsExportAccess vérifie que l'utilisateur peut exporter les données financières
func HasFinancialsExportAccess(user *auth.UserLoginRow) bool {
	return user.HasFinancialsExportAccess()
}

// HasCustomerManagementAccess vérifie que l'utilisateur peut gérer les clients
func HasCustomerManagementAccess(user *auth.UserLoginRow) bool {
	return user.HasCustomerManagementAccess()
}

// HasCustomerExportAccess vérifie que l'utilisateur peut exporter les clients
func HasCustomerExportAccess(user *auth.UserLoginRow) bool {
	return user.HasCustomerExportAccess()
}

// ============================================================
// PERMISSION COMBINERS
// Helpers pour combiner plusieurs permissions avec logique OR
// ============================================================

// AnyOf retourne une PermissionFunc qui vérifie si AU MOINS UNE des permissions est vraie (logique OR)
func AnyOf(permissions ...PermissionFunc) PermissionFunc {
	return func(user *auth.UserLoginRow) bool {
		for _, hasPermission := range permissions {
			if hasPermission(user) {
				return true
			}
		}
		return false
	}
}

// AllOf retourne une PermissionFunc qui vérifie si TOUTES les permissions sont vraies (logique AND)
// Note : RequirePermission fait déjà un AND par défaut, donc ceci est surtout utile pour composer
func AllOf(permissions ...PermissionFunc) PermissionFunc {
	return func(user *auth.UserLoginRow) bool {
		for _, hasPermission := range permissions {
			if !hasPermission(user) {
				return false
			}
		}
		return true
	}
}

// ============================================================
// EXEMPLES D'USAGE
// ============================================================

/*
// 1. Permission simple
r.Route("/menu", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(middleware.HasMenuAccess))

    r.Post("/product", menuH.CreateProduct)
})

// 2. Permissions multiples (AND) - L'utilisateur doit avoir TOUS les droits
r.Route("/admin", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(
        middleware.IsAdmin,
        middleware.HasSettingsAccess,
    ))

    r.Get("/settings", adminH.GetSettings)
})

// 3. Permissions alternatives (OR) - L'utilisateur doit avoir AU MOINS UN droit
r.Route("/cash", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Use(middleware.RequirePermission(
        middleware.AnyOf(
            middleware.IsAdmin,
            middleware.HasAccessReception,
            middleware.CanPrintCashReport,
        ),
    ))

    r.Get("/reports", cashH.GetReports)
})

// 4. Permission sur une seule route
r.With(authMiddleware).
  With(middleware.RequirePermission(middleware.HasMenuAccess)).
  Post("/menu/product", menuH.CreateProduct)

// 5. Permission personnalisée inline
r.Use(middleware.RequirePermission(func(user *auth.UserLoginRow) bool {
    return user.MerchantID == "specific-merchant-id"
}))
*/
