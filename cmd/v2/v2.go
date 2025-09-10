package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	// "golang.org/x/net/proxy"
)

type target struct {
	raw      string // what the user provided (label)
	hostport string // normalized host:port to dial
}

type sample struct {
	t   time.Time
	dur time.Duration
	err error
}

type ring struct {
	mu      sync.Mutex
	samples []sample
	max     int // cap on displayed history (for -history)
}

func (r *ring) push(s sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, s)
	if len(r.samples) > r.max {
		r.samples = r.samples[len(r.samples)-r.max:]
	}
}

func (r *ring) snapshot() []sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sample, len(r.samples))
	copy(out, r.samples)
	return out
}

// windowStats returns min/avg/max over the given time window.
// Only successful (err==nil) samples are considered for latency stats.
// Returns ok=false if there are no successful samples in the window.
func (r *ring) windowStats(window time.Duration) (min, avg, max time.Duration, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-window)
	var total time.Duration
	var count int
	var mn, mx time.Duration
	for _, s := range r.samples {
		if s.t.Before(cutoff) {
			continue
		}
		if s.err != nil {
			continue
		}
		if count == 0 {
			mn, mx = s.dur, s.dur
		} else {
			if s.dur < mn {
				mn = s.dur
			}
			if s.dur > mx {
				mx = s.dur
			}
		}
		total += s.dur
		count++
	}
	if count == 0 {
		return 0, 0, 0, false
	}
	return mn, time.Duration(int64(total) / int64(count)), mx, true
}

// ---------- Flags & config ----------

type config struct {
	interval    time.Duration
	timeout     time.Duration
	defaultPort int
	once        bool
	historyLen  int
	window      time.Duration
	color       bool
	warnThresh  time.Duration
	critThresh  time.Duration
	proxySpec   string // "", "system", or URL
}

func parseFlags() config {
	intervalStr := flag.String("interval", "2s", "interval between pings, e.g. 500ms, 1s, 2s")
	flag.StringVar(intervalStr, "i", *intervalStr, "alias for -interval")

	timeoutStr := flag.String("timeout", "1s", "timeout per TCP attempt")
	flag.StringVar(timeoutStr, "t", *timeoutStr, "alias for -timeout")

	defaultPort := flag.Int("port", 80, "default port when none specified and no scheme found")
	flag.IntVar(defaultPort, "p", *defaultPort, "alias for -port")

	once := flag.Bool("once", false, "run a single iteration then exit")

	historyLen := flag.Int("history", 5, "how many recent results to show per target")
	windowStr := flag.String("window", "30m", "rolling stats window for min/avg/max (e.g. 10m, 1h)")
	warnStr := flag.String("warn", "150ms", "warn threshold for latency color")
	critStr := flag.String("crit", "500ms", "critical threshold for latency color")
	noColor := flag.Bool("no-color", false, "disable colorized output")

	proxySpec := flag.String("proxy", "", "proxy to use: empty (none), 'system', or URL (http(s):// or socks5://)")

	flag.Parse()

	interval, err := time.ParseDuration(*intervalStr)
	if err != nil || interval <= 0 {
		fmt.Fprintf(os.Stderr, "invalid interval: %v\n", *intervalStr)
		os.Exit(2)
	}
	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil || timeout <= 0 {
		fmt.Fprintf(os.Stderr, "invalid timeout: %v\n", *timeoutStr)
		os.Exit(2)
	}
	window, err := time.ParseDuration(*windowStr)
	if err != nil || window <= 0 {
		fmt.Fprintf(os.Stderr, "invalid window: %v\n", *windowStr)
		os.Exit(2)
	}
	warn, err := time.ParseDuration(*warnStr)
	if err != nil || warn <= 0 {
		fmt.Fprintf(os.Stderr, "invalid warn: %v\n", *warnStr)
		os.Exit(2)
	}
	crit, err := time.ParseDuration(*critStr)
	if err != nil || crit <= 0 {
		fmt.Fprintf(os.Stderr, "invalid crit: %v\n", *critStr)
		os.Exit(2)
	}
	if *historyLen < 1 {
		*historyLen = 1
	}

	return config{
		interval:    interval,
		timeout:     timeout,
		defaultPort: *defaultPort,
		once:        *once,
		historyLen:  *historyLen,
		window:      window,
		color:       !*noColor,
		warnThresh:  warn,
		critThresh:  crit,
		proxySpec:   *proxySpec,
	}
}

// ---------- Terminal rendering (per-line updates) ----------

type renderer struct {
	mu    sync.Mutex
	lines []string // last rendered text for each line
	color bool
}

func newRenderer(n int, color bool) *renderer {
	r := &renderer{
		lines: make([]string, n),
		color: color,
	}
	// Prepare screen: print n lines
	fmt.Print(hideCursor())
	for i := 0; i < n; i++ {
		fmt.Println()
	}
	// Keep cursor at bottom after initialization
	return r
}

// update a single line (0-based)
func (r *renderer) update(i int, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Move cursor up from bottom to the target line
	up := len(r.lines) - i
	fmt.Printf("\x1b[%dA", up) // move up 'up' lines
	fmt.Print("\r\x1b[2K")     // CR + clear line
	fmt.Print(text)
	// Move back down to bottom
	down := up - 1
	if down > 0 {
		fmt.Printf("\x1b[%dB", down)
	}
	fmt.Print("\r")
	r.lines[i] = text
}

func (r *renderer) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Print(showCursor())
}

// ---------- Colors ----------

const (
	clrReset   = "\x1b[0m"
	clrGreen   = "\x1b[32m"
	clrYellow  = "\x1b[33m"
	clrRed     = "\x1b[31m"
	clrMagenta = "\x1b[35m"
	clrCyan    = "\x1b[36m"
	clrGray    = "\x1b[90m"
)

func hideCursor() string { return "\x1b[?25l" }
func showCursor() string { return "\x1b[?25h" }

func colorize(s string, color bool, code string) string {
	if !color {
		return s
	}
	return code + s + clrReset
}

func colorForLatency(d, warn, crit time.Duration) string {
	if d <= warn {
		return clrGreen
	}
	if d <= crit {
		return clrYellow
	}
	return clrRed
}

// ---------- Proxy dialing ----------

type dialFunc func(network, address string, timeout time.Duration) (net.Conn, error)

func buildDialer(proxySpec string) (dialFunc, error) {
	if proxySpec == "" {
		return func(network, address string, timeout time.Duration) (net.Conn, error) {
			d := net.Dialer{Timeout: timeout}
			return d.Dial(network, address)
		}, nil
	}

	var proxyURL *url.URL
	if proxySpec == "system" {
		// Prefer ALL_PROXY, then HTTPS_PROXY (common for TLS), then HTTP_PROXY
		envs := []string{"ALL_PROXY", "all_proxy", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"}
		var raw string
		for _, e := range envs {
			if v := os.Getenv(e); v != "" {
				raw = v
				break
			}
		}
		if raw == "" {
			return nil, fmt.Errorf("no system proxy env (ALL_PROXY/HTTPS_PROXY/HTTP_PROXY) found")
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse system proxy: %w", err)
		}
		proxyURL = u
	} else {
		u, err := url.Parse(proxySpec)
		if err != nil {
			return nil, fmt.Errorf("parse proxy: %w", err)
		}
		proxyURL = u
	}

	switch strings.ToLower(proxyURL.Scheme) {
	// case "socks5", "socks5h":
	// 	return func(network, address string, timeout time.Duration) (net.Conn, error) {
	// 		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, &net.Dialer{Timeout: timeout})
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		type timeoutDialer interface {
	// 			DialTimeout(network, addr string, timeout time.Duration) (net.Conn, error)
	// 		}
	// 		// x/net/proxy Dialer doesn't expose DialTimeout; use a context with deadline via net.Dialer in underlying
	// 		// but the interface doesn't carry timeout; we approximate by using a separate goroutine + timer.
	// 		done := make(chan struct{})
	// 		var c net.Conn
	// 		var dErr error
	// 		go func() {
	// 			c, dErr = dialer.Dial(network, address)
	// 			close(done)
	// 		}()
	// 		select {
	// 		case <-done:
	// 			return c, dErr
	// 		case <-time.After(timeout):
	// 			return nil, &net.DNSError{Err: "SOCKS5 dial timeout", Name: address, IsTimeout: true}
	// 		}
	// 	}, nil
	case "http", "https":
		return func(network, address string, timeout time.Duration) (net.Conn, error) {
			// HTTP CONNECT tunneling
			d := net.Dialer{Timeout: timeout}
			conn, err := d.Dial("tcp", proxyURL.Host)
			if err != nil {
				return nil, err
			}
			// Optional: Proxy auth via URL userinfo
			host := address
			var auth string
			if proxyURL.User != nil {
				if pw, ok := proxyURL.User.Password(); ok {
					auth = "Proxy-Authorization: Basic " + basicAuth(proxyURL.User.Username(), pw) + "\r\n"
				}
			}
			req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", host, host, auth)
			if _, err := io.WriteString(conn, req); err != nil {
				conn.Close()
				return nil, err
			}
			tp := textproto.NewReader(bufio.NewReader(conn))
			status, err := tp.ReadLine()
			if err != nil {
				conn.Close()
				return nil, err
			}
			// Read headers to reach the blank line
			for {
				line, err := tp.ReadLine()
				if err != nil {
					conn.Close()
					return nil, err
				}
				if line == "" {
					break
				}
			}
			if !strings.Contains(status, "200") {
				conn.Close()
				return nil, fmt.Errorf("proxy CONNECT failed: %s", status)
			}
			return conn, nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
}

func basicAuth(user, pass string) string {
	// Minimal inline base64 (avoid importing encoding/base64 if desired).
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(user + ":" + pass)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var n uint32
		remain := len(b) - i
		switch {
		case remain >= 3:
			n = uint32(b[i])<<16 | uint32(b[i+1])<<8 | uint32(b[i+2])
			out.WriteByte(enc[(n>>18)&63])
			out.WriteByte(enc[(n>>12)&63])
			out.WriteByte(enc[(n>>6)&63])
			out.WriteByte(enc[n&63])
		case remain == 2:
			n = uint32(b[i])<<16 | uint32(b[i+1])<<8
			out.WriteByte(enc[(n>>18)&63])
			out.WriteByte(enc[(n>>12)&63])
			out.WriteByte(enc[(n>>6)&63])
			out.WriteByte('=')
		case remain == 1:
			n = uint32(b[i]) << 16
			out.WriteByte(enc[(n>>18)&63])
			out.WriteByte(enc[(n>>12)&63])
			out.WriteByte('=')
			out.WriteByte('=')
		}
	}
	return out.String()
}

// ---------- Main ----------

func main() {
	cfg := parseFlags()

	// Targets
	inputs := flag.Args()
	if len(inputs) == 0 {
		inputs = []string{
			"https://google.com",
			"https://github.com",
			"https://cloudflare.com",
		}
	}
	var targets []target
	for _, in := range inputs {
		tgt, err := parseTarget(in, cfg.defaultPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %q: %v\n", in, err)
			continue
		}
		targets = append(targets, tgt)
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "no valid targets")
		os.Exit(2)
	}

	// Dialer (with optional proxy)
	dial, err := buildDialer(cfg.proxySpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy error: %v\n", err)
		os.Exit(2)
	}

	// Graceful exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Per-target histories and renderer
	hists := make([]*ring, len(targets))
	for i := range hists {
		hists[i] = &ring{max: maxInt(cfg.historyLen, 1)}
	}

	if cfg.once {
		// Single-shot ping and print once per target
		lines := make([]string, len(targets))
		var wg sync.WaitGroup
		wg.Add(len(targets))
		for i := range targets {
			i := i
			go func() {
				defer wg.Done()
				dur, err := tcpPingWith(dial, targets[i].hostport, cfg.timeout)
				hists[i].push(sample{t: time.Now(), dur: dur, err: err})
				lines[i] = renderLine(cfg, targets[i], hists[i])
			}()
		}
		wg.Wait()
		for _, ln := range lines {
			fmt.Println(ln)
		}
		return
	}

	// Continuous mode
	r := newRenderer(len(targets), cfg.color)
	defer r.close()

	// Initial paint
	for i := range targets {
		r.update(i, fmt.Sprintf("%s %s", labelFor(targets[i]), colorize("…", cfg.color, clrGray)))
	}

	// Start per-target workers
	var wg sync.WaitGroup
	wg.Add(len(targets))
	stop := make(chan struct{})

	for i := range targets {
		i := i
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(cfg.interval)
			defer ticker.Stop()

			// First immediate ping without waiting for interval
			doPingAndRender(cfg, i, targets[i], hists[i], r, dial)

			for {
				select {
				case <-ticker.C:
					doPingAndRender(cfg, i, targets[i], hists[i], r, dial)
				case <-stop:
					return
				}
			}
		}()
	}

	// Wait for signal
	<-sigCh
	close(stop)
	wg.Wait()
	fmt.Println()
}

func doPingAndRender(cfg config, idx int, tgt target, hist *ring, r *renderer, dial dialFunc) {
	start := time.Now()
	dur, err := tcpPingWith(dial, tgt.hostport, cfg.timeout)
	hist.push(sample{t: start, dur: dur, err: err})
	r.update(idx, renderLine(cfg, tgt, hist))
}

func renderLine(cfg config, tgt target, hist *ring) string {
	parts := []string{labelFor(tgt)}

	// History display (most recent first)
	sn := hist.snapshot()
	if len(sn) > 0 {
		// show up to cfg.historyLen, newest last for readability
		limit := cfg.historyLen
		if len(sn) < limit {
			limit = len(sn)
		}
		items := make([]string, 0, limit)
		// show last N in chronological order
		start := len(sn) - limit
		for _, s := range sn[start:] {
			items = append(items, colorizeSample(cfg, s))
		}
		parts = append(parts, fmt.Sprintf("[%s]", strings.Join(items, " ")))
	} else {
		parts = append(parts, "[]")
	}

	// Rolling stats over window
	if mn, avg, mx, ok := hist.windowStats(cfg.window); ok {
		minS := colorize(formatDuration(mn), cfg.color, colorForLatency(mn, cfg.warnThresh, cfg.critThresh))
		avgS := colorize(formatDuration(avg), cfg.color, clrCyan)
		maxS := colorize(formatDuration(mx), cfg.color, colorForLatency(mx, cfg.warnThresh, cfg.critThresh))
		parts = append(parts, fmt.Sprintf("win(%s) min/avg/max=%s/%s/%s",
			cfg.window, minS, avgS, maxS))
	} else {
		parts = append(parts, fmt.Sprintf("win(%s) min/avg/max=—/—/—", cfg.window))
	}

	return strings.Join(parts, "  ")
}

func colorizeSample(cfg config, s sample) string {
	if s.err != nil {
		// Distinguish TIMEOUT vs other
		if nerr, ok := s.err.(net.Error); ok && nerr.Timeout() {
			return colorize("TIMEOUT", cfg.color, clrMagenta)
		}
		return colorize("ERR", cfg.color, clrRed)
	}
	return colorize(formatDuration(s.dur), cfg.color, colorForLatency(s.dur, cfg.warnThresh, cfg.critThresh))
}

func labelFor(t target) string {
	if strings.Contains(t.raw, "://") {
		return t.raw
	}
	return t.hostport
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return d.Truncate(time.Microsecond).String()
	}
	return d.Truncate(time.Millisecond).String()
}

func tcpPingWith(dial dialFunc, addr string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	conn, err := dial("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

// ---------- Target parsing (unchanged with added schemes) ----------

func parseTarget(input string, fallbackPort int) (target, error) {
	// URL form
	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err != nil {
			return target{}, err
		}
		if u.Host == "" {
			return target{}, fmt.Errorf("missing host")
		}
		host := u.Host
		if _, _, err := net.SplitHostPort(host); err != nil {
			p := defaultPortForScheme(u.Scheme)
			if p == 0 {
				p = fallbackPort
			}
			host = net.JoinHostPort(host, fmt.Sprintf("%d", p))
		}
		return target{raw: input, hostport: host}, nil
	}
	// host:port form
	if _, _, err := net.SplitHostPort(input); err == nil {
		return target{raw: input, hostport: input}, nil
	}
	// bare host -> add fallbackPort
	return target{raw: input, hostport: net.JoinHostPort(input, fmt.Sprintf("%d", fallbackPort))}, nil
}

func defaultPortForScheme(s string) int {
	switch strings.ToLower(s) {
	case "http":
		return 80
	case "https":
		return 443
	case "ssh":
		return 22
	case "postgres", "postgresql":
		return 5432
	case "mysql":
		return 3306
	case "redis":
		return 6379
	case "amqp", "rabbitmq":
		return 5672
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
