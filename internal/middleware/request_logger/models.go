package requestlogger

type LogEntry struct {
	UserID     *int64
	MerchantID *string
	Method     string
	URL        string
	Payload    []byte
	StatusCode int
	IP         string
}
