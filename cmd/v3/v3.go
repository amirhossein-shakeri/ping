package main

import "time"

const (
	DEFAULT_BUFFER_SIZE          = 16
	DEFAULT_MIN_REQUEST_INTERVAL = 1
	DEFAULT_MAX_REQUEST_INTERVAL = 120
)

type Target struct {
	Address string // 1.1.1.1 | google.com
	Config  *TargetConfig
	Pings   []time.Duration
}

type TargetConfig struct {
	Interval time.Duration
	// todo: interval grow/shrink func?
}

var defaultDomains = []Target{}

func main() {
	// Accept CLI args to specify domains/IPs and buffer size
}
