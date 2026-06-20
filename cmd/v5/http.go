package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

type SafeHTTPClient struct {
	client       *http.Client
	timeout      time.Duration
	userAgent    string
	maxBodyBytes int64
}

func NewHTTPClient(proxyURL *url.URL, cfg Config) *SafeHTTPClient {
	dialer := &net.Dialer{
		Timeout:   cfg.Timeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		ExpectContinueTimeout: time.Second,
		DisableKeepAlives:     true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureTLS,
		},
	}

	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &SafeHTTPClient{
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		timeout:      cfg.Timeout,
		userAgent:    cfg.UserAgent,
		maxBodyBytes: cfg.MaxBodyBytes,
	}
}

func detectProxy() *url.URL {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		raw := strings.TrimSpace(getEnv(key))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		return parsed
	}
	return nil
}

func getEnv(key string) string {
	return strings.TrimSpace(strings.TrimSpace(strings.Trim(os.Getenv(key), `"`)))
}

func (client *SafeHTTPClient) Probe(parent context.Context, row *RowState, mode Mode) ProbeResult {
	startedAt := time.Now()
	result := ProbeResult{
		StartedAt: startedAt,
		Title:     row.Title,
		Address:   row.Address,
		Path:      row.Path,
		Scheme:    row.Scheme,
		Method:    row.Method,
		Mode:      mode,
		PingMS:    ErrUnknown,
	}

	ctx, cancel := context.WithTimeout(parent, client.timeout)
	defer cancel()

	targetURL := fmt.Sprintf("%s://%s%s", strings.ToLower(string(row.Scheme)), row.Address, row.Path)
	req, err := http.NewRequestWithContext(ctx, string(row.Method), targetURL, nil)
	if err != nil {
		result.PingMS = ErrRequestBuild
		result.ErrorText = err.Error()
		result.FinishedAt = time.Now()
		return result
	}

	req.Header.Set("User-Agent", client.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "close")

	result.SentBytes = estimateRequestBytes(req)

	start := time.Now()
	resp, err := client.client.Do(req)
	if err != nil {
		result.PingMS = mapProbeError(err, mode)
		result.ErrorText = err.Error()
		result.FinishedAt = time.Now()
		result.TotalBytes = result.SentBytes
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	received, readErr := drainResponse(resp, client.maxBodyBytes)
	result.RecvBytes = received
	result.ResponseSize = received
	result.TotalBytes = result.SentBytes + result.RecvBytes

	if readErr != nil {
		result.PingMS = ErrReadFailure
		result.ErrorText = readErr.Error()
		result.FinishedAt = time.Now()
		return result
	}
	if resp.StatusCode >= 400 {
		result.PingMS = ErrHTTPFailure
		result.ErrorText = resp.Status
		result.FinishedAt = time.Now()
		return result
	}

	result.PingMS = int(time.Since(start).Milliseconds())
	result.FinishedAt = time.Now()
	return result
}

func drainResponse(resp *http.Response, limit int64) (int64, error) {
	if resp == nil || resp.Body == nil {
		return 0, nil
	}
	reader := io.Reader(resp.Body)
	if limit > 0 {
		reader = io.LimitReader(resp.Body, limit)
	}
	written, err := io.Copy(io.Discard, reader)
	if err != nil {
		return written, err
	}
	return written + estimateResponseHeaderBytes(resp), nil
}

func estimateRequestBytes(req *http.Request) int64 {
	if req == nil || req.URL == nil {
		return 0
	}
	var total int64
	total += int64(len(req.Method) + 1 + len(req.URL.RequestURI()) + len(" HTTP/1.1\r\n"))
	total += int64(len("Host: ") + len(req.Host) + len("\r\n"))
	for key, values := range req.Header {
		for _, value := range values {
			total += int64(len(key) + 2 + len(value) + 2)
		}
	}
	total += 2
	if req.ContentLength > 0 {
		total += req.ContentLength
	}
	return total
}

func estimateResponseHeaderBytes(resp *http.Response) int64 {
	if resp == nil {
		return 0
	}
	total := int64(len(resp.Proto) + 1 + len(resp.Status) + 2)
	for key, values := range resp.Header {
		for _, value := range values {
			total += int64(len(key) + 2 + len(value) + 2)
		}
	}
	total += 2
	return total
}

func mapProbeError(err error, mode Mode) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrDNSError
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "proxyconnect" || strings.Contains(strings.ToLower(opErr.Err.Error()), "proxy") {
			return ErrProxyFailure
		}
		var syscallErr *os.SyscallError
		if errors.As(opErr.Err, &syscallErr) && errors.Is(syscallErr.Err, syscall.ECONNREFUSED) {
			return ErrConnRefused
		}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if strings.Contains(strings.ToLower(urlErr.Err.Error()), "tls") || strings.Contains(strings.ToLower(urlErr.Err.Error()), "certificate") {
			return ErrTLS
		}
		if mode == ModeProxy && strings.Contains(strings.ToLower(urlErr.Err.Error()), "proxy") {
			return ErrProxyFailure
		}
	}

	if strings.Contains(strings.ToLower(err.Error()), "tls") {
		return ErrTLS
	}
	if mode == ModeProxy && strings.Contains(strings.ToLower(err.Error()), "proxy") {
		return ErrProxyFailure
	}
	return ErrUnknown
}
