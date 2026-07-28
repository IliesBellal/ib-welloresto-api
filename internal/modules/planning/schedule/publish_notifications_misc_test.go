package schedule

import (
	"testing"
	"time"
)

func TestShouldSendInlinePlanningSMS(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-6 * 24 * time.Hour)
	stale := now.Add(-8 * 24 * time.Hour)
	if shouldSendInlinePlanningSMS(&recent, now) {
		t.Fatal("expected recent activity to avoid inline SMS")
	}
	if !shouldSendInlinePlanningSMS(&stale, now) {
		t.Fatal("expected stale activity to trigger inline SMS")
	}
	if !shouldSendInlinePlanningSMS(nil, now) {
		t.Fatal("expected missing activity to trigger inline SMS")
	}
}
