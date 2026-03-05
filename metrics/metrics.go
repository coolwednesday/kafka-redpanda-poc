package metrics

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Collector struct {
	mu       sync.Mutex
	samples  []time.Duration
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Record(d time.Duration) {
	c.mu.Lock()
	c.samples = append(c.samples, d)
	c.mu.Unlock()
}

func (c *Collector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.samples)
}

func (c *Collector) Percentile(p float64) time.Duration {
	c.mu.Lock()
	sorted := make([]time.Duration, len(c.samples))
	copy(sorted, c.samples)
	c.mu.Unlock()

	if len(sorted) == 0 {
		return 0
	}

	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (c *Collector) P50() time.Duration { return c.Percentile(0.50) }
func (c *Collector) P95() time.Duration { return c.Percentile(0.95) }

type Result struct {
	Broker      string
	Concurrency int
	Total       int
	Elapsed     time.Duration
	Throughput  float64
	P50         time.Duration
	P95         time.Duration
	E2EP50      time.Duration
	E2EP95      time.Duration
	Errors      int64
	Warmup      int
}

func fmtDuration(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fus", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}

func PrintSummary(r Result) {
	sep := strings.Repeat("=", 66)
	dash := strings.Repeat("-", 66)

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("  Kafka vs Redpanda Benchmark Results")
	fmt.Println(sep)
	fmt.Printf("%-12s %-12s %-10s %-14s %-10s %-10s\n",
		"Broker", "Concurrency", "Total", "Throughput", "p50", "p95")
	fmt.Println(dash)
	fmt.Printf("%-12s %-12d %-10d %-14s %-10s %-10s\n",
		r.Broker,
		r.Concurrency,
		r.Total,
		fmt.Sprintf("%.0f/s", r.Throughput),
		fmtDuration(r.P50),
		fmtDuration(r.P95),
	)
	fmt.Println(dash)
	fmt.Printf("E2E Latency (consumer):  p50 = %s  |  p95 = %s\n",
		fmtDuration(r.E2EP50), fmtDuration(r.E2EP95))
	fmt.Printf("Errors: %d  |  Warm-up discarded: %d\n", r.Errors, r.Warmup)
	fmt.Println(sep)
	fmt.Println()
}

func ExportCSV(filename string, r Result) error {
	_, err := os.Stat(filename)
	writeHeader := os.IsNotExist(err)

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if writeHeader {
		if err := w.Write([]string{
			"broker", "concurrency", "total", "throughput_msg_s",
			"p50_ms", "p95_ms", "e2e_p50_ms", "e2e_p95_ms", "errors",
		}); err != nil {
			return fmt.Errorf("write csv header: %w", err)
		}
	}

	return w.Write([]string{
		r.Broker,
		fmt.Sprintf("%d", r.Concurrency),
		fmt.Sprintf("%d", r.Total),
		fmt.Sprintf("%.0f", r.Throughput),
		fmt.Sprintf("%.2f", float64(r.P50.Microseconds())/1000.0),
		fmt.Sprintf("%.2f", float64(r.P95.Microseconds())/1000.0),
		fmt.Sprintf("%.2f", float64(r.E2EP50.Microseconds())/1000.0),
		fmt.Sprintf("%.2f", float64(r.E2EP95.Microseconds())/1000.0),
		fmt.Sprintf("%d", r.Errors),
	})
}
