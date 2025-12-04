package orders

type Notifier interface {
	SendNewOrderNotification(orderID int64)
}

type FakeNotifier struct{}

func (FakeNotifier) SendNewOrderNotification(orderID int64) {
	// TODO real implementation later
}
