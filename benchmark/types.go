package benchmark

import (
	"runtime"
	"sort"
	"time"
)

// Result captures the statistical performance profile of a benchmark run.
type Result struct {
	Name         string        `json:"name"`
	Iterations   int           `json:"iterations"`
	TotalTime    time.Duration `json:"total_time"`
	P50          time.Duration `json:"p50"`
	P90          time.Duration `json:"p90"`
	P99          time.Duration `json:"p99"`
	Mean         time.Duration `json:"mean"`
	Min          time.Duration `json:"min"`
	Max          time.Duration `json:"max"`
	AllocsPerOp  uint64        `json:"allocs_per_op"`
	BytesPerOp   uint64        `json:"bytes_per_op"`
	Throughput   float64       `json:"throughput"` // ops per second
	PeakRSSMB    float64       `json:"peak_rss_mb,omitempty"`
}

// CalculateStats computes percentiles and allocation statistics.
func CalculateStats(name string, latencies []time.Duration, mStart, mEnd runtime.MemStats) Result {
	n := len(latencies)
	if n == 0 {
		return Result{Name: name}
	}

	sorted := make([]time.Duration, n)
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, l := range sorted {
		total += l
	}

	p50 := sorted[int(float64(n)*0.50)]
	p90 := sorted[int(float64(n)*0.90)]
	p99 := sorted[int(float64(n)*0.99)]
	mean := total / time.Duration(n)
	min := sorted[0]
	max := sorted[n-1]

	allocs := uint64(0)
	bytesAlloc := uint64(0)
	if mEnd.Mallocs >= mStart.Mallocs {
		allocs = (mEnd.Mallocs - mStart.Mallocs) / uint64(n)
	}
	if mEnd.TotalAlloc >= mStart.TotalAlloc {
		bytesAlloc = (mEnd.TotalAlloc - mStart.TotalAlloc) / uint64(n)
	}

	throughput := float64(0)
	if total > 0 {
		throughput = float64(n) / total.Seconds()
	}

	peakRSS := float64(mEnd.Sys) / (1024 * 1024)

	return Result{
		Name:        name,
		Iterations:  n,
		TotalTime:   total,
		P50:         p50,
		P90:         p90,
		P99:         p99,
		Mean:        mean,
		Min:         min,
		Max:         max,
		AllocsPerOp: allocs,
		BytesPerOp:  bytesAlloc,
		Throughput:  throughput,
		PeakRSSMB:   peakRSS,
	}
}
