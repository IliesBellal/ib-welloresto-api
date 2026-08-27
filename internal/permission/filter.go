package permission

// FilterValid keeps only the keys present in the live catalog (All),
// dropping anything else. A defensive net for RBAC lot 9's login-response
// Permissions field: role_permissions.permission_key is already FK'd to the
// permissions table (see docs/decisions.md's account of how lot 8 retired
// pos.access/pos.discount.apply), so this should never actually filter
// anything out in practice — it exists to make that invariant explicit and
// testable rather than relying solely on the DB constraint holding forever
// across future catalog changes.
func FilterValid(keys []string) []string {
	valid := make(map[string]bool, len(All))
	for _, k := range All {
		valid[string(k)] = true
	}

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if valid[k] {
			out = append(out, k)
		}
	}
	return out
}
