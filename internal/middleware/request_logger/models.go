package requestlogger

type LogEntry struct {
	UserID     *int64
	MerchantID *int64
	Method     string
	URL        string
	Payload    []byte
	StatusCode int
	IP         string
}
