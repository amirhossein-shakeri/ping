package main

import "time"

type Target struct {
	Address string // 1.1.1.1 | google.com
	Config  *TargetConfig
	Pings   []PingResult
}

type TargetConfig struct {
	Methods  []string // OPTIONS | HEAD | GET
	Interval time.Duration
	// todo: interval grow/shrink func?
}

type PingResult struct {
	Method     string
	Error      error
	Latency    time.Duration
	StatusCode int
	Success    bool
}
