package order_life_cycle

type DeliveredOrderMetadata struct {
	Brand           string
	BrandOrderID    *string
	MerchantID      string
	FulfillmentType string
}

type ProductionStatusProduct struct {
	OrderItemID      string `json:"order_item_id"`
	OrderID          string `json:"order_id"`
	ProductionStatus string `json:"production_status"`
}

type UpdateProductionStatusRequest struct {
	Products []ProductionStatusProduct `json:"products"`
}

type SendInvoiceEmailRequest struct {
	Email      string  `json:"email"`
	FirstName  string  `json:"prenom"`
	LastName   string  `json:"nom"`
	CustomerID *string `json:"customer_id"`
}

type SendInvoiceEmailResponse struct {
	CustomerID  string `json:"customer_id"`
	EmailSentTo string `json:"email_sent_to"`
}

// EmailDeliveryError signale que le lien client a bien été enregistré, mais que l'envoi
// de l'email (Brevo) a échoué. Le handler doit le distinguer des autres erreurs pour
// renvoyer customer_linked=true au staff.
type EmailDeliveryError struct {
	Err error
}

func (e *EmailDeliveryError) Error() string {
	return e.Err.Error()
}

func (e *EmailDeliveryError) Unwrap() error {
	return e.Err
}
