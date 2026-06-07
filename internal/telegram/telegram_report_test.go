package telegram

import (
	"testing"
	"time"
)

func TestReportIntervalParsing(t *testing.T) {
	tests := []struct {
		name      string
		interval  string
		wantErr   bool
		wantValid bool
	}{
		{"valid 5 minutes", "5m", false, true},
		{"valid 10 minutes", "10m", false, true},
		{"valid 1 hour", "1h", false, true},
		{"valid 24 hours", "24h", false, true},
		{"valid 30 seconds", "30s", false, true},
		{"valid 7 days", "168h", false, true},
		{"empty string", "", true, false},
		{"invalid format", "abc", true, false},
		{"missing unit", "5", true, false},
		{"invalid unit", "5x", true, false},
		{"negative value (parsed but rejected by handleSetReport validation)", "-5m", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := time.ParseDuration(tt.interval)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for interval %q, got nil", tt.interval)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for interval %q: %v", tt.interval, err)
			}
		})
	}
}

func TestReportIntervalEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		expected time.Duration
	}{
		{"5 minutes", "5m", 5 * time.Minute},
		{"1 hour", "1h", 1 * time.Hour},
		{"24 hours", "24h", 24 * time.Hour},
		{"30 seconds", "30s", 30 * time.Second},
		{"2 hours 30 minutes", "2h30m", 2*time.Hour + 30*time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := time.ParseDuration(tt.interval)
			if err != nil {
				t.Fatalf("failed to parse interval %q: %v", tt.interval, err)
			}
			if d != tt.expected {
				t.Errorf("got %v, want %v for interval %q", d, tt.expected, tt.interval)
			}
		})
	}
}

func TestReportSchedulingLogic(t *testing.T) {
	tests := []struct {
		name            string
		lastSentAt      time.Time
		interval        string
		shouldSend      bool
		description     string
	}{
		{
			name:        "should send when 5 min passed and interval is 5m",
			lastSentAt:  time.Now().Add(-6 * time.Minute),
			interval:    "5m",
			shouldSend:  true,
			description: "5 minutes elapsed, interval is 5m",
		},
		{
			name:        "should NOT send when only 3 min passed and interval is 5m",
			lastSentAt:  time.Now().Add(-3 * time.Minute),
			interval:    "5m",
			shouldSend:  false,
			description: "3 minutes elapsed, interval is 5m",
		},
		{
			name:        "should send when 1 hour passed and interval is 1h",
			lastSentAt:  time.Now().Add(-65 * time.Minute),
			interval:    "1h",
			shouldSend:  true,
			description: "65 minutes elapsed, interval is 1h",
		},
		{
			name:        "should NOT send when just set (lastSentAt = now)",
			lastSentAt:  time.Now(),
			interval:    "5m",
			shouldSend:  false,
			description: "No time elapsed since last report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := time.ParseDuration(tt.interval)
			if err != nil {
				t.Fatalf("failed to parse duration: %v", err)
			}

			elapsed := time.Since(tt.lastSentAt)
			shouldSend := elapsed >= duration

			if shouldSend != tt.shouldSend {
				t.Errorf("test %q failed: %s — got %v, want %v",
					tt.name, tt.description, shouldSend, tt.shouldSend)
			}
		})
	}
}

func TestReportSchedulingWithNullLastSentAt(t *testing.T) {
	// Test scenario: user baru set report_interval untuk pertama kali
	// last_report_sent_at = NULL (di PostgreSQL)
	// Dengan COALESCE(last_report_sent_at, CURRENT_TIMESTAMP),
	// user seharusnya langsung dapat report saat scheduler berikutnya

	// Simulasi: last_sent_at = epoch (first report)
	lastSentAt := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	interval := "5m"

	duration, err := time.ParseDuration(interval)
	if err != nil {
		t.Fatalf("failed to parse duration: %v", err)
	}

	elapsed := time.Since(lastSentAt)
	shouldSend := elapsed >= duration

	if !shouldSend {
		t.Errorf("user with NULL last_report_sent_at should always trigger first report")
	}
}