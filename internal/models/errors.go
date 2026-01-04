package models

import "errors"

var (
	ErrDeliverySessionAlreadyActive = errors.New("delivery_session_already_active")
	ErrInvalidToken                 = errors.New("invalid_token")
)
