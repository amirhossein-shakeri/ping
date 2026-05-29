package main

import (
	"fmt"
	"net/http"
	"time"
)

type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(timeout time.Duration) *HTTPClient {
	fmt.Println("Initializing new HTTP client")

	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}
