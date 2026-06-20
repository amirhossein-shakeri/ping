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
 *
 * Proxy detected: none
 * Press Ctrl+C to stop.
 *
 * | Uptime 3h24m39s . Total requests sent: 5,283 . Success(Direct): 4,792 . Success(Proxy): - . Failed(Direct): 491 . Failed(Proxy): - |
 * | Total traffic usage: 33.2 MiB . Direct traffic usage: 33.2 MiB . Proxy traffic usage: - |
 *
 * | Title ?? Address | Scheme | Method  | Ping(ms) Buffer                                | Proxy Ping(ms) Buffer                       | AVG | Proxy AVG |
 * | ---------------- | ------ | ------- | ---------------------------------------------- | ------------------------------------------- | --- | --------- |
 * | Cloudflare       | HTTP   | GET     | 23 25 -1 28 30 -1 -1 35 1200 -1 22 24 -1 26    | 41 39 42 -1 44 40 -1 46 45 -1 47 43 -1 48   | 185 | 43        |
 * |                  | HTTP   | HEAD    | 25 27 24 -1 32 30 -1 -1 2000 -1 24 26 -1 28    | 43 41 44 -1 46 42 -1 48 47 -1 49 45 -1 50   | 286 | 45        |
 * |                  | HTTPS  | GET     | 24 26 -1 29 31 -1 -1 36 1500 -1 23 25 -1 27    | 42 40 43 -1 45 41 -1 47 46 -1 48 44 -1 49   | 220 | 44        |
 * |                  | HTTPS  | HEAD    | 26 28 25 -1 33 32 -1 -1 2500 -1 25 27 -1 29    | 44 42 45 -1 47 43 -1 49 48 -1 50 46 -1 51   | 344 | 46        |
 * | Google           | HTTP   | GET     | 32 35 -1 38 40 -1 5000 -1 42 45 -1 34 36 -1    | 55 58 60 -1 62 59 -1 64 63 -1 65 61 -1 66   | 750 | 61        |
 * |                  | HTTP   | HEAD    | 34 37 -1 40 42 -1 6000 -1 44 47 -1 36 38 -1    | 57 60 62 -1 64 61 -1 66 65 -1 67 63 -1 68   | 880 | 63        |
 * |                  | HTTPS  | GET     | 33 36 -1 39 41 -1 5500 -1 43 46 -1 35 37 -1    | 56 59 61 -1 63 60 -1 65 64 -1 66 62 -1 67   | 810 | 62        |
 * |                  | HTTPS  | HEAD    | 35 38 -1 41 43 -1 6500 -1 45 48 -1 37 39 -1    | 58 61 63 -1 65 62 -1 67 66 -1 68 64 -1 69   | 950 | 64        |
 * | GitHub           | HTTP   | GET     | 28 -1 30 35 40 -1 1200 1342 -1 32 35 -1 38     | 50 48 52 -1 55 53 -1 57 56 -1 59 54 -1 60   | 315 | 54        |
 * |                  | HTTP   | HEAD    | 30 -1 32 37 42 -1 1500 1600 -1 34 37 -1 40     | 52 50 54 -1 57 55 -1 59 58 -1 61 56 -1 62   | 380 | 56        |
 * |                  | HTTPS  | GET     | 29 -1 31 36 41 -1 1350 1450 -1 33 36 -1 39     | 51 49 53 -1 56 54 -1 58 57 -1 60 55 -1 61   | 340 | 55        |
 * |                  | HTTPS  | HEAD    | 31 -1 33 38 43 -1 1800 1900 -1 35 38 -1 41     | 53 51 55 -1 58 56 -1 60 59 -1 62 57 -1 63   | 450 | 57        |
 * | 1.1.1.1          | HTTP   | GET     | 22 21 -1 24 25 -1 30 32 -1 35 28 -1 30 9       | 38 37 39 -1 41 40 -1 42 43 -1 44 41 -1 45   | 26  | 41        |
 * |                  | HTTP   | HEAD    | 24 23 -1 26 27 -1 32 34 -1 37 30 -1 32 11      | 40 39 41 -1 43 42 -1 44 45 -1 46 43 -1 47   | 29  | 43        |
 * |                  | HTTPS  | GET     | 23 22 -1 25 26 -1 31 33 -1 36 29 -1 31 10      | 39 38 40 -1 42 41 -1 43 44 -1 45 42 -1 46   | 28  | 42        |
 * |                  | HTTPS  | HEAD    | 25 24 -1 27 28 -1 33 35 -1 38 31 -1 33 12      | 41 40 42 -1 44 43 -1 45 46 -1 47 44 -1 48   | 32  | 44        |
 * | 8.8.8.8          | HTTP   | GET     | 31 30 -1 33 35 -1 38 40 -1 42 37 -1 40 8       | 49 47 50 -1 52 51 -1 53 54 -1 55 52 -1 56   | 35  | 52        |
 * |                  | HTTP   | HEAD    | 33 32 -1 35 37 -1 40 42 -1 44 39 -1 42 10      | 51 49 52 -1 54 53 -1 55 56 -1 57 54 -1 58   | 38  | 54        |
 * |                  | HTTPS  | GET     | 32 31 -1 34 36 -1 39 41 -1 43 38 -1 41 9       | 50 48 51 -1 53 52 -1 54 55 -1 56 53 -1 57   | 36  | 53        |
 * |                  | HTTPS  | HEAD    | 34 33 -1 36 38 -1 41 43 -1 45 40 -1 43 11      | 52 50 53 -1 55 54 -1 56 57 -1 58 55 -1 59   | 40  | 55        |
 * | 4.2.2.4          | HTTP   | GET     | 45 44 -1 48 50 -1 52 55 -1 58 53 -1 55 12      | 63 61 65 -1 67 66 -1 68 69 -1 70 66 -1 72   | 51  | 66        |
 * |                  | HTTP   | HEAD    | 47 46 -1 50 52 -1 54 57 -1 60 55 -1 57 14      | 65 63 67 -1 69 68 -1 70 71 -1 72 68 -1 74   | 54  | 68        |
 * |                  | HTTPS  | GET     | 46 45 -1 49 51 -1 53 56 -1 59 54 -1 56 13      | 64 62 66 -1 68 67 -1 69 70 -1 71 67 -1 73   | 52  | 67        |
 * |                  | HTTPS  | HEAD    | 48 47 -1 51 53 -1 55 58 -1 61 56 -1 58 15      | 66 64 68 -1 70 69 -1 71 72 -1 73 69 -1 75   | 55  | 69        |
 * | example.com      | HTTP   | GET     | 60 62 -1 65 68 -1 72 75 -1 78 70 -1 75 9       | 82 80 84 -1 86 85 -1 87 88 -1 89 85 -1 90   | 68  | 85        |
 * |                  | HTTP   | HEAD    | 62 64 -1 67 70 -1 74 77 -1 80 72 -1 77 11      | 84 82 86 -1 88 87 -1 89 90 -1 91 87 -1 92   | 71  | 87        |
 * |                  | HTTPS  | GET     | 61 63 -1 66 69 -1 73 76 -1 79 71 -1 76 10      | 83 81 85 -1 87 86 -1 88 89 -1 90 86 -1 91   | 69  | 86        |
 * |                  | HTTPS  | HEAD    | 63 65 -1 68 71 -1 75 78 -1 81 73 -1 78 12      | 85 83 87 -1 89 88 -1 90 91 -1 92 88 -1 93   | 72  | 88        |
 * | Firefox          | HTTPS  | GET     | 41 40 -1 43 45 -1 47 50 -1 52 48 -1 49 9       | 60 58 62 -1 64 63 -1 66 67 -1 68 63 -1 69   | 45  | 64        |
 * | GStatic          | HTTPS  | HEAD    | 27 26 -1 29 31 -1 33 35 -1 37 32 -1 34 9       | 44 42 45 -1 47 46 -1 48 49 -1 50 46 -1 51   | 32  | 47        |
 * | Cloudflare Trace | HTTP   | OPTIONS | 24 23 -1 26 28 -1 30 32 -1 34 29 -1 31 9       | 40 38 41 -1 43 42 -1 44 45 -1 46 42 -1 47   | 29  | 43        |
 * | Microsoft        | HTTPS  | GET     | 52 50 -1 54 57 -1 60 63 -1 65 59 -1 61 9       | 73 71 75 -1 77 76 -1 78 79 -1 80 76 -1 82   | 57  | 76        |
 * | YouTube          | HTTPS  | GET     | 33 34 -1 37 40 -1 42 45 -1 48 41 -1 43 9       | 55 53 57 -1 59 58 -1 60 61 -1 62 58 -1 63   | 41  | 58        |
 * | LinkedIn         | HTTPS  | HEAD    | 70 68 -1 72 75 -1 78 81 -1 84 77 -1 79 9       | 95 92 97 -1 100 98 -1 101 102 -1 104 99 -1  | 76  | 99        |
 * | go.dev           | HTTPS  | GET     | 37 36 -1 39 42 -1 45 48 -1 51 44 -1 46 9       | 57 55 59 -1 61 60 -1 62 63 -1 64 60 -1 65   | 44  | 60        |
 * | google.cn        | HTTP   | OPTIONS | 90 92 -1 98 105 -1 120 150 -1 200 180 -1 210   | 140 150 160 -1 170 180 -1 190 200 -1 210    | 152 | 180       |
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
