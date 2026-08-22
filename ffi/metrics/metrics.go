// Package metrics exposes the small process measurements used by spikes.
package metrics

import (
	"runtime"
	"time"
)

// HeapAllocBytes returns bytes allocated in the Go heap at this instant.
func HeapAllocBytes() int {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return int(stats.HeapAlloc)
}

// Nanoseconds returns a duration as the platform-sized integer Ard uses.
func Nanoseconds(elapsed time.Duration) int {
	return int(elapsed.Nanoseconds())
}

// AverageNanoseconds returns an integer average for a measured duration.
func AverageNanoseconds(elapsed time.Duration, iterations int) int {
	if iterations <= 0 {
		return 0
	}
	return int(elapsed.Nanoseconds() / int64(iterations))
}
