package stripeclient

import (
	"github.com/stripe/stripe-go/v84"
)

type Client struct{}

func New(secretKey string) *Client {
	stripe.Key = secretKey
	return &Client{}
}
