package main

import (
	"net/url"
	"sync"
	"time"
)

type Scheme string
type Method string
type Mode string

const (
	SchemeHTTP  Scheme = "HTTP"
	SchemeHTTPS Scheme = "HTTPS"

	MethodGet     Method = "GET"
	MethodHead    Method = "HEAD"
	MethodOptions Method = "OPTIONS"

	ModeDirect Mode = "direct"
	ModeProxy  Mode = "proxy"
)

const (
	ErrUnknown         = -1
	ErrRequestBuild    = -2
	ErrTimeout         = -3
	ErrDNSError        = -4
	ErrConnRefused     = -5
	ErrTLS             = -6
	ErrProxyFailure    = -7
	ErrHTTPFailure     = -8
	ErrReadFailure     = -9
	historyBucketCount = 720
)

type RowKey struct {
	Address string
	Scheme  Scheme
	Method  Method
	Mode    Mode
}

type ProbeResult struct {
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	Title        string    `json:"title"`
	Address      string    `json:"address"`
	Path         string    `json:"path"`
	Scheme       Scheme    `json:"scheme"`
	Method       Method    `json:"method"`
	Mode         Mode      `json:"mode"`
	PingMS       int       `json:"ping_ms"`
	StatusCode   int       `json:"status_code"`
	SentBytes    int64     `json:"sent_bytes"`
	RecvBytes    int64     `json:"recv_bytes"`
	TotalBytes   int64     `json:"total_bytes"`
	ResponseSize int64     `json:"response_size"`
	ErrorText    string    `json:"error,omitempty"`
}

type RowState struct {
	Title   string
	Address string
	Path    string
	Scheme  Scheme
	Method  Method

	mu     sync.RWMutex
	Direct *ModeState
	Proxy  *ModeState
}

type ModeState struct {
	Buffer  *Ring[int]
	Running bool
	Latest  ProbeResult
	Count   int64
	Success int64
	Failed  int64
	Traffic int64
}

func NewRowState(target Target, scheme Scheme, method Method, bufferSize int, withProxy bool) *RowState {
	row := &RowState{
		Title:   target.Title,
		Address: target.Address,
		Path:    target.Path,
		Scheme:  scheme,
		Method:  method,
		Direct:  &ModeState{Buffer: NewRing[int](bufferSize)},
	}
	if withProxy {
		row.Proxy = &ModeState{Buffer: NewRing[int](bufferSize)}
	}
	return row
}

func (row *RowState) modeState(mode Mode) *ModeState {
	switch mode {
	case ModeDirect:
		return row.Direct
	case ModeProxy:
		return row.Proxy
	default:
		return nil
	}
}

type App struct {
	cfg        Config
	startedAt  time.Time
	proxyURL   *url.URL
	withProxy  bool
	rows       []*RowState
	directHTTP *SafeHTTPClient
	proxyHTTP  *SafeHTTPClient
	snapshots  *HistoryStore
	logger     *ResultLogger
}
