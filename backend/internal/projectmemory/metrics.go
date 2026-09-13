package projectmemory

import (
	"expvar"
	"sync/atomic"
	"time"
)

type MetricsSnapshot struct {
	QueueDepth          int64 `json:"queue_depth"`
	JobsCompleted       int64 `json:"jobs_completed"`
	JobDurationCount    int64 `json:"job_duration_count"`
	JobDurationTotalMS  int64 `json:"job_duration_total_ms"`
	JobDurationMaxMS    int64 `json:"job_duration_max_ms"`
	LeaseExpiries       int64 `json:"lease_expiries"`
	Retries             int64 `json:"retries"`
	CandidatesSubmitted int64 `json:"candidates_submitted"`
	CandidatesAccepted  int64 `json:"candidates_accepted"`
	Conflicts           int64 `json:"conflicts"`
	RetrievalCount      int64 `json:"retrieval_count"`
	RetrievalTotalMS    int64 `json:"retrieval_total_ms"`
	RetrievalMaxMS      int64 `json:"retrieval_max_ms"`
	InjectedTokens      int64 `json:"injected_tokens"`
	CacheFallbacks      int64 `json:"cache_fallbacks"`
}

type metrics struct {
	queueDepth, jobsCompleted, jobDurationCount, jobDurationTotalMS atomic.Int64
	jobDurationMaxMS, leaseExpiries, retries                        atomic.Int64
	candidatesSubmitted, candidatesAccepted, conflicts              atomic.Int64
	retrievalCount, retrievalTotalMS, retrievalMaxMS                atomic.Int64
	injectedTokens, cacheFallbacks                                  atomic.Int64
}

var DefaultMetrics metrics

func observeMax(target *atomic.Int64, value int64) {
	for current := target.Load(); value > current; current = target.Load() {
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

func (m *metrics) SetQueueDepth(value int) { m.queueDepth.Store(int64(value)) }
func (m *metrics) LeaseExpired()           { m.leaseExpiries.Add(1) }
func (m *metrics) Retried()                { m.retries.Add(1) }
func (m *metrics) Conflict()               { m.conflicts.Add(1) }
func (m *metrics) CacheFallback()          { m.cacheFallbacks.Add(1) }
func (m *metrics) Injected(tokenCount int) { m.injectedTokens.Add(int64(tokenCount)) }

func (m *metrics) JobCompleted(duration time.Duration) {
	value := duration.Milliseconds()
	m.jobsCompleted.Add(1)
	m.jobDurationCount.Add(1)
	m.jobDurationTotalMS.Add(value)
	observeMax(&m.jobDurationMaxMS, value)
}

func (m *metrics) Candidates(submitted, accepted int) {
	m.candidatesSubmitted.Add(int64(submitted))
	m.candidatesAccepted.Add(int64(accepted))
}

func (m *metrics) Retrieval(duration time.Duration) {
	value := duration.Milliseconds()
	m.retrievalCount.Add(1)
	m.retrievalTotalMS.Add(value)
	observeMax(&m.retrievalMaxMS, value)
}

func (m *metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		QueueDepth: m.queueDepth.Load(), JobsCompleted: m.jobsCompleted.Load(),
		JobDurationCount: m.jobDurationCount.Load(), JobDurationTotalMS: m.jobDurationTotalMS.Load(),
		JobDurationMaxMS: m.jobDurationMaxMS.Load(), LeaseExpiries: m.leaseExpiries.Load(),
		Retries: m.retries.Load(), CandidatesSubmitted: m.candidatesSubmitted.Load(),
		CandidatesAccepted: m.candidatesAccepted.Load(), Conflicts: m.conflicts.Load(),
		RetrievalCount: m.retrievalCount.Load(), RetrievalTotalMS: m.retrievalTotalMS.Load(),
		RetrievalMaxMS: m.retrievalMaxMS.Load(), InjectedTokens: m.injectedTokens.Load(),
		CacheFallbacks: m.cacheFallbacks.Load(),
	}
}

func SnapshotMetrics() MetricsSnapshot { return DefaultMetrics.Snapshot() }

func init() {
	expvar.Publish("project_memory", expvar.Func(func() any { return SnapshotMetrics() }))
}
