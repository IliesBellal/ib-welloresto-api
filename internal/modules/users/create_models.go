package users

// CreateUserRequest is the JSON payload for POST /users/create.
type CreateUserRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	UserName  string `json:"username"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	Tel       string `json:"tel"`
}

// CreateUserResponse is the JSON body returned on success (201).
type CreateUserResponse struct {
	UserID string `json:"user_id"`
}
