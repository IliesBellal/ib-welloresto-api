package rbacobserve

// Observation is one RequirePermission decision: who, on which route, asked
// for which permission, and whether it was granted. Aggregated into
// access_observation (merchant_id, user_id, permission_key, route) with a
// hit counter — see migrations/done/098_access_observation.up.sql.
type Observation struct {
	MerchantID    string
	UserID        string
	PermissionKey string
	Route         string
	Granted       bool
}
