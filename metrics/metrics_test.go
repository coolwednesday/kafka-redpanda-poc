package metrics

import (
	"os"
	"testing"
	"time"
)

func TestPercentiles(t *testing.T) {
	c := NewCollector()
	// Add 100 samples: 1ms, 2ms, ..., 100ms
	for i := 1; i <= 100; i++ {
		c.Record(time.Duration(i) * time.Millisecond)
	}

	p50 := c.P50()
	p95 := c.P95()

	// p50 should be ~50ms (index 49 of 0-99)
	if p50 < 49*time.Millisecond || p50 > 51*time.Millisecond {
		t.Errorf("P50: expected ~50ms, got %v", p50)
	}

	// p95 should be ~95ms (index 94 of 0-99)
	if p95 < 94*time.Millisecond || p95 > 96*time.Millisecond {
		t.Errorf("P95: expected ~95ms, got %v", p95)
	}
}

func TestPercentilesEmpty(t *testing.T) {
	c := NewCollector()
	if c.P50() != 0 {
		t.Error("P50 of empty collector should be 0")
	}
	if c.P95() != 0 {
		t.Error("P95 of empty collector should be 0")
	}
}

func TestPercentilesSingle(t *testing.T) {
	c := NewCollector()
	c.Record(5 * time.Millisecond)
	if c.P50() != 5*time.Millisecond {
		t.Errorf("P50: expected 5ms, got %v", c.P50())
	}
	if c.P95() != 5*time.Millisecond {
		t.Errorf("P95: expected 5ms, got %v", c.P95())
	}
}

func TestExportCSV(t *testing.T) {
	tmpFile := t.TempDir() + "/test_results.csv"

	r := Result{
		Broker:      "kafka",
		Concurrency: 4,
		Total:       1000,
		Throughput:  5000,
		P50:         1200 * time.Microsecond,
		P95:         3800 * time.Microsecond,
		E2EP50:      5100 * time.Microsecond,
		E2EP95:      12400 * time.Microsecond,
		Errors:      0,
	}

	if err := ExportCSV(tmpFile, r); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Fatal("CSV file is empty")
	}

	// Write a second row
	r.Broker = "redpanda"
	if err := ExportCSV(tmpFile, r); err != nil {
		t.Fatalf("ExportCSV second: %v", err)
	}

	data, _ = os.ReadFile(tmpFile)
	content = string(data)
	// Should have header + 2 data rows = 3 non-empty lines
	lines := 0
	for _, line := range splitLines(content) {
		if line != "" {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 lines (header + 2 rows), got %d", lines)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
