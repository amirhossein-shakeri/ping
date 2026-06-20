package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Scheme string
type Method string

const (
	SchemeHTTP  Scheme = "HTTP"
	SchemeHTTPS Scheme = "HTTPS"

	MethodGET     Method = "GET"
	MethodHEAD    Method = "HEAD"
	MethodOPTIONS Method = "OPTIONS"
)

type Target struct {
	Title   string
	Address string
	Schemes []Scheme
	Methods []Method
	Path    string
}

type Mode string

const (
	ModeDirect Mode = "direct"
	ModeProxy  Mode = "proxy"
)

type RowKey struct {
	Title   string
	Address string
	Scheme  Scheme
	Method  Method
}

type RowState struct {
	Title   string
	Address string
	Scheme  Scheme
	Method  Method

	Direct *RollingBuffer
	Proxy  *RollingBuffer
}

type RollingBuffer struct {
	mu      sync.RWMutex
	values  []int
	size    int
	next    int
	filled  bool
	running bool
}

func NewRollingBuffer(size int) *RollingBuffer {
	return &RollingBuffer{
		values: make([]int, size),
		size:   size,
	}
}

func (b *RollingBuffer) Add(v int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.values[b.next] = v
	b.next++
	if b.next >= b.size {
		b.next = 0
		b.filled = true
	}
}

func (b *RollingBuffer) SetRunning(r bool) {
	b.mu.Lock()
	b.running = r
	b.mu.Unlock()
}

func (b *RollingBuffer) Snapshot() []int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.filled {
		out := make([]int, b.next)
		copy(out, b.values[:b.next])
		return out
	}

	out := make([]int, b.size)
	copy(out, b.values[b.next:])
	copy(out[b.size-b.next:], b.values[:b.next])
	return out
}

func (b *RollingBuffer) Average() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var vals []int
	if !b.filled {
		vals = b.values[:b.next]
	} else {
		vals = b.values
	}

	sum := 0
	count := 0

	for _, v := range vals {
		if v >= 0 {
			sum += v
			count++
		}
	}

	if count == 0 {
		return -1
	}

	return sum / count
}

func (b *RollingBuffer) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

func main() {
	var (
		interval   = flag.Duration("interval", 2*time.Second, "request interval per row")
		timeout    = flag.Duration("timeout", 5*time.Second, "request timeout")
		bufferSize = flag.Int("buffer", 14, "rolling buffer size")
		noColor    = flag.Bool("no-color", false, "disable ANSI colors")
	)

	flag.Parse()

	proxyURL := detectProxy()

	directClient := newHTTPClient(nil, *timeout)

	var proxyClient *http.Client
	if proxyURL != nil {
		proxyClient = newHTTPClient(proxyURL, *timeout)
	}

	targets := defaultTargets()

	rows := buildRows(targets, *bufferSize, proxyURL != nil)

	ctx, cancel := signalContext()
	defer cancel()

	for _, row := range rows {
		go rowWorker(ctx, row, ModeDirect, directClient, *interval)

		if proxyURL != nil {
			go rowWorker(ctx, row, ModeProxy, proxyClient, *interval)
		}
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	hideCursor()
	defer showCursor()

	for {
		select {
		case <-ctx.Done():
			clearScreen()
			render(rows, proxyURL, *noColor)
			fmt.Println()
			return

		case <-ticker.C:
			clearScreen()
			render(rows, proxyURL, *noColor)
		}
	}
}

func defaultTargets() []Target {
	return []Target{
		{
			Title:   "Cloudflare",
			Address: "cloudflare.com",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "Google",
			Address: "google.com",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "GitHub",
			Address: "github.com",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "1.1.1.1",
			Address: "1.1.1.1",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "8.8.8.8",
			Address: "8.8.8.8",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "4.2.2.4",
			Address: "4.2.2.4",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "example.com",
			Address: "example.com",
			Schemes: []Scheme{SchemeHTTP, SchemeHTTPS},
			Methods: []Method{MethodGET, MethodHEAD},
		},
		{
			Title:   "Firefox",
			Address: "detectportal.firefox.com",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodGET},
		},
		{
			Title:   "GStatic",
			Address: "www.gstatic.com",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodHEAD},
		},
		{
			Title:   "Cloudflare Trace",
			Address: "cloudflare.com",
			Schemes: []Scheme{SchemeHTTP},
			Methods: []Method{MethodOPTIONS},
			Path:    "/cdn-cgi/trace",
		},
		{
			Title:   "Microsoft",
			Address: "microsoft.com",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodGET},
		},
		{
			Title:   "YouTube",
			Address: "youtube.com",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodGET},
		},
		{
			Title:   "LinkedIn",
			Address: "linkedin.com",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodHEAD},
		},
		{
			Title:   "go.dev",
			Address: "go.dev",
			Schemes: []Scheme{SchemeHTTPS},
			Methods: []Method{MethodGET},
		},
		{
			Title:   "google.cn",
			Address: "google.cn",
			Schemes: []Scheme{SchemeHTTP},
			Methods: []Method{MethodOPTIONS},
		},
	}
}

func buildRows(targets []Target, bufferSize int, withProxy bool) []*RowState {
	var rows []*RowState

	for _, t := range targets {
		for _, scheme := range t.Schemes {
			for _, method := range t.Methods {
				r := &RowState{
					Title:   t.Title,
					Address: t.Address,
					Scheme:  scheme,
					Method:  method,
					Direct:  NewRollingBuffer(bufferSize),
				}

				if withProxy {
					r.Proxy = NewRollingBuffer(bufferSize)
				}

				rows = append(rows, r)
			}
		}
	}

	return rows
}

func rowWorker(
	ctx context.Context,
	row *RowState,
	mode Mode,
	client *http.Client,
	interval time.Duration,
) {
	// Stagger workers slightly so they do not all fire at once.
	time.Sleep(time.Duration(hashString(row.Title+string(row.Scheme)+string(row.Method)+string(mode))%1000) * time.Millisecond)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		runOnce(ctx, row, mode, client)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runOnce(ctx context.Context, row *RowState, mode Mode, client *http.Client) {
	var buffer *RollingBuffer

	switch mode {
	case ModeDirect:
		buffer = row.Direct
	case ModeProxy:
		buffer = row.Proxy
	default:
		return
	}

	if buffer == nil {
		return
	}

	buffer.SetRunning(true)
	defer buffer.SetRunning(false)

	ms := pingHTTP(ctx, client, row.Address, row.Scheme, row.Method)
	buffer.Add(ms)
}

func pingHTTP(
	parent context.Context,
	client *http.Client,
	address string,
	scheme Scheme,
	method Method,
) int {
	ctx, cancel := context.WithTimeout(parent, client.Timeout)
	defer cancel()

	targetURL := strings.ToLower(string(scheme)) + "://" + address

	req, err := http.NewRequestWithContext(ctx, string(method), targetURL, nil)
	if err != nil {
		return -1
	}

	req.Header.Set("User-Agent", "net-table-pinger/1.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "close")

	start := time.Now()

	resp, err := client.Do(req)
	if err != nil {
		return -1
	}

	_ = resp.Body.Close()

	elapsed := time.Since(start).Milliseconds()
	if elapsed < 0 {
		return -1
	}

	return int(elapsed)
}

func newHTTPClient(proxyURL *url.URL, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy: nil,

		DialContext: dialer.DialContext,

		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},

		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     true,
	}

	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func detectProxy() *url.URL {
	envs := []string{
		"HTTPS_PROXY",
		"https_proxy",
		"HTTP_PROXY",
		"http_proxy",
		"ALL_PROXY",
		"all_proxy",
	}

	for _, key := range envs {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}

		u, err := url.Parse(raw)
		if err != nil {
			continue
		}

		if u.Scheme == "" || u.Host == "" {
			continue
		}

		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5":
			return u
		}
	}

	return nil
}

func render(rows []*RowState, proxyURL *url.URL, noColor bool) {
	withProxy := proxyURL != nil

	fmt.Println("Async HTTP Reachability Table")
	fmt.Println()

	if withProxy {
		fmt.Printf("Proxy detected: %s\n", proxyURL.Redacted())
	} else {
		fmt.Println("Proxy detected: none")
	}

	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	if withProxy {
		renderWithProxy(rows, noColor)
	} else {
		renderDirectOnly(rows, noColor)
	}
}

func renderWithProxy(rows []*RowState, noColor bool) {
	headers := []string{
		"Title / Address",
		"Scheme",
		"Method",
		"Ping(ms) Buffer",
		"Proxy Ping(ms) Buffer",
		"AVG",
		"Proxy AVG",
	}

	table := newTable(headers)

	lastTitle := ""

	for _, r := range rows {
		title := r.Title
		if title == lastTitle {
			title = ""
		} else {
			lastTitle = r.Title
		}

		directBuffer := formatBuffer(r.Direct, noColor)
		proxyBuffer := formatBuffer(r.Proxy, noColor)

		table.Add([]string{
			title,
			string(r.Scheme),
			string(r.Method),
			directBuffer,
			proxyBuffer,
			formatAvg(r.Direct.Average(), noColor),
			formatAvg(r.Proxy.Average(), noColor),
		})
	}

	table.Render()
}

func renderDirectOnly(rows []*RowState, noColor bool) {
	headers := []string{
		"Title / Address",
		"Scheme",
		"Method",
		"Ping(ms) Buffer",
		"AVG",
	}

	table := newTable(headers)

	lastTitle := ""

	for _, r := range rows {
		title := r.Title
		if title == lastTitle {
			title = ""
		} else {
			lastTitle = r.Title
		}

		table.Add([]string{
			title,
			string(r.Scheme),
			string(r.Method),
			formatBuffer(r.Direct, noColor),
			formatAvg(r.Direct.Average(), noColor),
		})
	}

	table.Render()
}

func formatBuffer(buffer *RollingBuffer, noColor bool) string {
	if buffer == nil {
		return ""
	}

	values := buffer.Snapshot()
	parts := make([]string, 0, len(values)+1)

	for _, v := range values {
		parts = append(parts, formatPing(v, noColor))
	}

	if buffer.IsRunning() {
		if noColor {
			parts = append(parts, "…")
		} else {
			parts = append(parts, cyan("…"))
		}
	}

	return strings.Join(parts, " ")
}

func formatPing(v int, noColor bool) string {
	s := fmt.Sprintf("%d", v)

	if noColor {
		return s
	}

	if v < 0 {
		return red(s)
	}

	switch {
	case v < 100:
		return green(s)
	case v < 500:
		return yellow(s)
	default:
		return red(s)
	}
}

func formatAvg(v int, noColor bool) string {
	if v < 0 {
		if noColor {
			return "-1"
		}
		return red("-1")
	}

	s := fmt.Sprintf("%d", v)

	if noColor {
		return s
	}

	switch {
	case v < 100:
		return green(s)
	case v < 500:
		return yellow(s)
	default:
		return red(s)
	}
}

type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

func newTable(headers []string) *Table {
	widths := make([]int, len(headers))

	for i, h := range headers {
		widths[i] = visibleLen(h)
	}

	return &Table{
		headers: headers,
		widths:  widths,
	}
}

func (t *Table) Add(row []string) {
	t.rows = append(t.rows, row)

	for i, cell := range row {
		if i >= len(t.widths) {
			continue
		}

		l := visibleLen(cell)
		if l > t.widths[i] {
			t.widths[i] = l
		}
	}
}

func (t *Table) Render() {
	t.renderSeparator()
	t.renderRow(t.headers)
	t.renderSeparator()

	for _, row := range t.rows {
		t.renderRow(row)
	}

	t.renderSeparator()
}

func (t *Table) renderSeparator() {
	fmt.Print("|")

	for _, w := range t.widths {
		fmt.Print(" ")
		fmt.Print(strings.Repeat("-", w))
		fmt.Print(" |")
	}

	fmt.Println()
}

func (t *Table) renderRow(row []string) {
	fmt.Print("|")

	for i, cell := range row {
		w := t.widths[i]
		fmt.Print(" ")
		fmt.Print(cell)
		fmt.Print(strings.Repeat(" ", w-visibleLen(cell)))
		fmt.Print(" |")
	}

	fmt.Println()
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func hideCursor() {
	fmt.Print("\033[?25l")
}

func showCursor() {
	fmt.Print("\033[?25h")
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
	}()

	return ctx, cancel
}

func hashString(s string) uint32 {
	var h uint32 = 2166136261

	for _, c := range []byte(s) {
		h ^= uint32(c)
		h *= 16777619
	}

	return h
}

func visibleLen(s string) int {
	clean := stripANSI(s)
	return len([]rune(clean))
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c == '\033' {
			inEscape = true
			continue
		}

		if inEscape {
			if c >= '@' && c <= '~' {
				inEscape = false
			}
			continue
		}

		b.WriteByte(c)
	}

	return b.String()
}

func green(s string) string {
	return "\033[32m" + s + "\033[0m"
}

func yellow(s string) string {
	return "\033[33m" + s + "\033[0m"
}

func red(s string) string {
	return "\033[31m" + s + "\033[0m"
}

func cyan(s string) string {
	return "\033[36m" + s + "\033[0m"
}

// Optional helper if you later want deterministic sorted row output.
func sortRows(rows []*RowState) {
	sort.SliceStable(rows, func(i, j int) bool {
		a := rows[i]
		b := rows[j]

		if a.Title != b.Title {
			return a.Title < b.Title
		}

		if a.Scheme != b.Scheme {
			return a.Scheme < b.Scheme
		}

		return a.Method < b.Method
	})
}
