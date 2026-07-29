package users

// CreateUserRequest is the JSON payload for POST /users/create.
type CreateUserRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	//UserName   string                           `json:"username"` // removed from DB
	Email      string                           `json:"email"`
	Password   string                           `json:"password"`
	Tel        string                           `json:"tel"`
	MerchantID *string                          `json:"merchant_id,omitempty"` // optional: auto-link user to merchant if provided
	Admin      bool                             `json:"admin"`                 // if MerchantID is set, whether to link as admin or regular user
	Rights     *MerchantUserRightsUpsertRequest `json:"rights,omitempty"`
}

// CreateUserResponse is the JSON body returned on success (201).
type CreateUserResponse struct {
	UserID string `json:"user_id"`
}
