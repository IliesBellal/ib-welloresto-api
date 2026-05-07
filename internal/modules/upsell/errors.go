package upsell

import "errors"

var (
	// ErrSuggestionNotFound is returned when the requested suggestion row does not exist.
	ErrSuggestionNotFound = errors.New("upsell suggestion not found")

	// ErrSuggestionMerchantMismatch is returned when the caller's merchantID does not
	// match the merchant_id stored on the suggestion. The caller should log a security
	// warning when this occurs.
	ErrSuggestionMerchantMismatch = errors.New("upsell suggestion merchant mismatch")

	// ErrEmptyCart is returned when an upsell is requested for an empty cart.
	ErrEmptyCart = errors.New("cart is empty")
)
