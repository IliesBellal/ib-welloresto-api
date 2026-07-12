package bookingcore

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{"pending to confirmed is allowed", StatusPending, StatusConfirmed, false},
		{"pending to denied is allowed", StatusPending, StatusDenied, false},
		{"pending to cancelled is rejected (use deny instead)", StatusPending, StatusCancelled, true},
		{"confirmed to seated is allowed", StatusConfirmed, StatusSeated, false},
		{"confirmed to cancelled is allowed", StatusConfirmed, StatusCancelled, false},
		{"confirmed to no_show is allowed", StatusConfirmed, StatusNoShow, false},
		{"seated to completed is allowed", StatusSeated, StatusCompleted, false},
		{"seated to cancelled is allowed", StatusSeated, StatusCancelled, false},
		{"completed to confirmed is rejected (terminal state)", StatusCompleted, StatusConfirmed, true},
		{"cancelled to confirmed is rejected (terminal state)", StatusCancelled, StatusConfirmed, true},
		{"same status is a no-op", StatusConfirmed, StatusConfirmed, false},
		{"legacy status is normalized before checking", "ACCEPTED", StatusSeated, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CanTransition(%q, %q) error = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}
