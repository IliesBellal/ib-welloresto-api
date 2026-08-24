package requestlogger

type LogEntry struct {
	UserID     *int64
	MerchantID *string
	Method     string
	URL        string
	Payload    []byte
	StatusCode int
	IP         string
	// DurationMs est la durée de traitement de la requête en millisecondes.
	// Alimente api_request_logs.duration_ms (migration 088), seule mesure de
	// latence persistée : les logs zap n'en enregistrent aucune.
	DurationMs int64
}
