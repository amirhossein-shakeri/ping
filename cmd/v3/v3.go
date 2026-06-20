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

var targets = []Target{
	{Address: "cp.cloudflare.com", Title: "Cloudflare"},
	{Address: "google.com", Title: "Google"},
	{Address: "github.com", Title: "GitHub"},
	{Address: "1.1.1.1"},
	{Address: "8.8.8.8"},
	{Address: "4.2.2.4"},
	{Address: "example.com"},
	{Address: "detectportal.firefox.com/success.txt", Title: "Firefox"},
	{Address: "connectivitycheck.gstatic.com/generate_204", Title: "GStatic"},
	{Address: "www.cloudflare.com/cdn-cgi/trace", Title: "Cloudflare Trace"},
	{Address: "www.msftconnecttest.com/connecttest.txt", Title: "Microsoft"},
	{Address: "youtube.com", Title: "YouTube"},
	{Address: "linkedin.com", Title: "LinkedIn"},
	{Address: "go.dev", Title: "go.dev"},
	{Address: "google.cn", Title: "google.cn"},
}

// Desired output/result

/*
 * | Title ?? Address | Method | Ping(ms) Buffer                                |
 * | ---------------- | ------ | ---------------------------------------------- |
 * | Cloudflare       | HTTP   | 23 24 20 -1 26 -1 30 28 -1 -1 11,173 -1 -1 164 |
 * | Google           | HTTP   | 32 34 30 28 -1 -1 10,000 -1 -1 15,000 -1 -1 -1 |
 * | GitHub           | HTTP   | 28 28 28 28 27 30 135 173 30 -1 1,200 1,342 -1 |
 * | 1.1.1.1          | HTTP   | 22 21 23 24 22 25 -1 27 26 -1 31 29 -1 33 -1 9 |
 * | 8.8.8.8          | HTTP   | 31 30 29 33 32 -1 35 36 -1 38 34 -1 40 37 8 -1 |
 * | 4.2.2.4          | HTTP   | 45 44 46 48 -1 47 49 51 -1 52 50 -1 55 53 12 8 |
 * | example.com      | HTTP   | 60 62 58 59 61 -1 64 63 -1 66 65 -1 70 68 9 18 |
 * | Firefox          | HTTP   | 41 40 39 42 -1 43 44 46 -1 47 45 -1 49 48 9 18 |
 * | GStatic          | HTTP   | 27 26 25 28 29 -1 30 31 -1 33 32 -1 35 34 9 18 |
 * | Cloudflare Trace | HTTP   | 24 23 22 25 -1 26 27 29 -1 30 28 -1 32 31 9 18 |
 * | Microsoft        | HTTP   | 52 50 49 53 -1 54 56 58 -1 60 57 -1 62 59 9 18 |
 * | YouTube          | HTTP   | 33 34 31 30 -1 36 38 40 -1 42 39 -1 44 41 9 18 |
 * | LinkedIn         | HTTP   | 70 68 69 72 -1 74 76 78 -1 80 77 -1 82 79 9 18 |
 * | go.dev           | HTTP   | 37 36 35 38 -1 39 41 43 -1 45 42 -1 47 44 9 18 |
 * | google.cn        | HTTP   | 90 92 95 -1 98 101 -1 110 120 -1 150 180 -1 210|
 */

func main() {
	// Accept CLI args to specify domains/IPs and buffer size and interval override or config file path or proxy

	// Initialize the HTTP client
	client := http.Client{
		Timeout: DEFAULT_TIMEOUT * time.Second,
	}

	var maxTitleLength, maxAddressLength int
	for _, t := range targets {
		if len(t.Address) > maxAddressLength {
			maxAddressLength = len(t.Address)
		}
		if len(t.Title) > maxTitleLength {
			maxTitleLength = len(t.Title)
		}
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

				if !isHTTPS && !isHTTP {
					t.Address = "https://" + t.Address
					isHTTPS = true
				}

				// If not set, try HTTPS, fallback to HTTP
				req, err := http.NewRequestWithContext(context.TODO(), m, t.Address, nil)
				if err != nil {
					// todo: warn the issue

					if isHTTPS {
						t.Address = strings.Replace(t.Address, "https://", "http://", 1)
					} else if isHTTP {
						t.Address = strings.Replace(t.Address, "http://", "https://", 1)
					}

					req, err = http.NewRequestWithContext(context.TODO(), m, t.Address, nil)
					if err != nil {
						// todo: warn the issue
					}
				}

				start := time.Now()
				resp, err := client.Do(req)
				// todo: Should fallback to the other scheme(HTTPS | HTTP) after doing the request?
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
