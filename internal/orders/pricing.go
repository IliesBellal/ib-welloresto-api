package orders

type PricingEngine interface {
	Compute(req *CreateOrderRequest) (float64, float64, float64, error)
}

type DefaultPricingEngine struct{}

func (DefaultPricingEngine) Compute(req *CreateOrderRequest) (float64, float64, float64, error) {
	// TODO
	return 0, 0, 0, nil
}
