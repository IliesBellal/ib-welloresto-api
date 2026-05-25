package haccp

import "testing"

func TestComputeStatus(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min      float64
		max      float64
		expected string
	}{
		{name: "within range", value: 4.2, min: 2.0, max: 5.0, expected: "ok"},
		{name: "lower bound included", value: 2.0, min: 2.0, max: 5.0, expected: "ok"},
		{name: "upper bound included", value: 5.0, min: 2.0, max: 5.0, expected: "ok"},
		{name: "alert below min within 2", value: 0.5, min: 2.0, max: 5.0, expected: "alert"},
		{name: "alert above max within 2", value: 6.2, min: 2.0, max: 5.0, expected: "alert"},
		{name: "critical far below", value: -2.1, min: 2.0, max: 5.0, expected: "critical"},
		{name: "critical far above", value: 7.1, min: 2.0, max: 5.0, expected: "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeStatus(tt.value, tt.min, tt.max)
			if got != tt.expected {
				t.Fatalf("computeStatus() = %s, expected %s", got, tt.expected)
			}
		})
	}
}
