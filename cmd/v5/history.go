package main

import (
	"sync"
	"time"
)

type HistoryBucket struct {
	Timestamp time.Time
	Count     int64
	Success   int64
	Failures  int64
	Traffic   int64
	PingSum   int64
	PingCount int64
}

type HistorySeries struct {
	Buckets []HistoryBucket
}

func NewHistorySeries() *HistorySeries {
	return &HistorySeries{Buckets: make([]HistoryBucket, historyBucketCount)}
}

func (series *HistorySeries) Add(ts time.Time, result ProbeResult) {
	second := ts.Unix()
	index := int(second % int64(len(series.Buckets)))
	bucket := &series.Buckets[index]

	if bucket.Timestamp.Unix() != second {
		*bucket = HistoryBucket{Timestamp: time.Unix(second, 0)}
	}

	bucket.Count++
	bucket.Traffic += result.TotalBytes
	if result.PingMS >= 0 {
		bucket.Success++
		bucket.PingSum += int64(result.PingMS)
		bucket.PingCount++
	} else {
		bucket.Failures++
	}
}

type HistoryStore struct {
	mu      sync.RWMutex
	perMode map[Mode]*HistorySeries
}

func NewHistoryStore(withProxy bool) *HistoryStore {
	perMode := map[Mode]*HistorySeries{
		ModeDirect: NewHistorySeries(),
	}
	if withProxy {
		perMode[ModeProxy] = NewHistorySeries()
	}
	return &HistoryStore{perMode: perMode}
}

func (store *HistoryStore) Add(result ProbeResult) {
	store.mu.Lock()
	defer store.mu.Unlock()

	series := store.perMode[result.Mode]
	if series == nil {
		return
	}
	series.Add(result.FinishedAt, result)
}

type HistorySummary struct {
	Count       int64
	Success     int64
	Failures    int64
	Traffic     int64
	AvgPing     int
	AvgReqPerS  float64
	AvgTrafficS float64
}

func (store *HistoryStore) Summary(mode Mode, window time.Duration, now time.Time) HistorySummary {
	store.mu.RLock()
	defer store.mu.RUnlock()

	series := store.perMode[mode]
	if series == nil {
		return HistorySummary{AvgPing: ErrUnknown}
	}

	var count, success, failures, traffic, pingSum, pingCount int64
	cutoff := now.Add(-window)

	for index := range series.Buckets {
		bucket := series.Buckets[index]
		if bucket.Timestamp.IsZero() || bucket.Timestamp.Before(cutoff) || bucket.Timestamp.After(now) {
			continue
		}
		count += bucket.Count
		success += bucket.Success
		failures += bucket.Failures
		traffic += bucket.Traffic
		pingSum += bucket.PingSum
		pingCount += bucket.PingCount
	}

	summary := HistorySummary{
		Count:    count,
		Success:  success,
		Failures: failures,
		Traffic:  traffic,
		AvgPing:  ErrUnknown,
	}
	if pingCount > 0 {
		summary.AvgPing = int(pingSum / pingCount)
	}

	seconds := window.Seconds()
	if seconds > 0 {
		summary.AvgReqPerS = float64(count) / seconds
		summary.AvgTrafficS = float64(traffic) / seconds
	}
	return summary
}
