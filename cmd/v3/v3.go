package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	DEFAULT_TIMEOUT              = 30
	DEFAULT_BUFFER_SIZE          = 16
	DEFAULT_MIN_REQUEST_INTERVAL = 1
	DEFAULT_MAX_REQUEST_INTERVAL = 120
)

type Target struct {
	Address string // 1.1.1.1 | google.com
	Config  *TargetConfig
	Pings   []PingResult
}

type PingResult struct {
	Method     string
	Error      error
	Latency    time.Duration
	StatusCode int
	Success    bool
}

type TargetConfig struct {
	Methods  []string // OPTIONS | HEAD | GET
	Interval time.Duration
	// todo: interval grow/shrink func?
}

var targets = []Target{
	{Address: "cp.cloudflare.com"},
	{Address: "google.com"},
	{Address: "github.com"},
	// {Address: "1.1.1.1"},
	// {Address: "8.8.8.8"},
	// {Address: "4.2.2.4"},
	{Address: "example.com"},
	{Address: "detectportal.firefox.com/success.txt"},
	{Address: "connectivitycheck.gstatic.com/generate_204"},
	{Address: "www.cloudflare.com/cdn-cgi/trace"},
	{Address: "www.msftconnecttest.com/connecttest.txt"},
	{Address: "youtube.com"},
	{Address: "linkedin.com"},
	{Address: "go.dev"},
	{Address: "google.cn"},
}

//

func main() {
	// Accept CLI args to specify domains/IPs and buffer size and interval override or config file path or proxy

	// Initialize the HTTP client
	client := http.Client{
		Timeout: DEFAULT_TIMEOUT * time.Second,
	}

	for {
		// Loop through targets
		for i := range targets {
			t := targets[i]

			// Skip if address is not provided/configured/present
			if t.Address == "" {
				// todo: Warn
				continue
			}

			// todo: default methods could be configurable
			// Set default methods
			methods := []string{http.MethodHead} // http.MethodConnect

			// If methods have been configured, use them instead of default methods
			if t.Config != nil && len(t.Config.Methods) > 0 {
				methods = t.Config.Methods
			}

			// Loop through methods
			for _, m := range methods {
				result := PingResult{Method: m}
				fmt.Printf("%48s[%7s]", t.Address, m)

				isHTTPS := strings.HasPrefix(t.Address, "https://")
				isHTTP := strings.HasPrefix(t.Address, "http://")

				// If not set, try HTTPS, fallback to HTTP
				req, err := http.NewRequestWithContext(context.TODO(), m, t.Address, nil)
				if err != nil {
					// todo: warn the issue
				}

				start := time.Now()
				resp, err := client.Do(req)
				result.Latency = time.Since(start)
				if err != nil {
					result.Error = err
					result.Latency = -2
					fmt.Printf("\tFailed: %v\n", err)
					time.Sleep(time.Second)
					continue
				}
				defer resp.Body.Close()

				result.StatusCode = resp.StatusCode
				result.Success = resp.StatusCode >= 200 && resp.StatusCode < 500

				t.Pings = append(t.Pings, result)

				fmt.Printf("\t%3d ms (%d)\n", result.Latency.Milliseconds(), result.StatusCode)
			}

			time.Sleep(time.Second)
		}

		// fmt.Println(targets)

		//
	}
}
