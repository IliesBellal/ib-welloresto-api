package haccp

func computeStatus(value, min, max float64) string {
	if value >= min && value <= max {
		return "ok"
	}

	if value >= (min-2.0) && value <= (max+2.0) {
		return "alert"
	}

	return "critical"
}
