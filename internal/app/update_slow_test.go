package app

import (
	"strings"
	"testing"
	"time"
)

func TestSlowUpdateWarning(t *testing.T) {
	m := &OS{}
	m.reportSlowUpdate("fast", slowUpdateThreshold/2)
	if len(m.LogMessages) != 0 {
		t.Fatalf("a fast update was logged: %+v", m.LogMessages)
	}
	m.reportSlowUpdate("slow", slowUpdateThreshold+time.Millisecond)
	if len(m.LogMessages) != 1 {
		t.Fatalf("a slow update logged %d entries, want 1", len(m.LogMessages))
	}
	entry := m.LogMessages[0]
	if entry.Level != "WARN" || !strings.Contains(entry.Message, "SLOW_UPDATE") {
		t.Fatalf("slow update log entry = %+v", entry)
	}
}
