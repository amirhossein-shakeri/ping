package main

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type target struct {
	raw      string // what the user provided (label)
	hostport string // normalized host:port to dial
}

func main() {
	// Flags
	intervalStr := flag.String("interval", "2s", "interval between pings, e.g. 500ms, 1s, 2s")
	flag.StringVar(intervalStr, "i", *intervalStr, "alias for -interval")
	timeoutStr := flag.String("timeout", "1s", "timeout per TCP attempt")
	flag.StringVar(timeoutStr, "t", *timeoutStr, "alias for -timeout")
	defaultPort := flag.Int("port", 80, "default port when none specified and no scheme found")
	flag.IntVar(defaultPort, "p", *defaultPort, "alias for -port")
	once := flag.Bool("once", false, "run a single iteration then exit")
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

	inputs := flag.Args()
	if len(inputs) == 0 {
		inputs = []string{
			"https://google.com",
			"https://github.com",
			"https://cloudflare.com",
			// "example.com", // will use -port if not a URL or host:port
		}
	}

	targets := make([]target, 0, len(inputs))
	for _, in := range inputs {
		tgt, err := parseTarget(in, *defaultPort)
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

	// Handle Ctrl+C gracefully to end the line with a newline
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println() // finish the current line
		os.Exit(0)
	}()

	lastLen := 0
	for {
		statuses := pingAll(targets, timeout)
		line := strings.Join(statuses, " | ")
		if pad := lastLen - len(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		fmt.Printf("\r%s", line)
		lastLen = len(line)

		if *once {
			fmt.Println()
			return
		}
		time.Sleep(interval)
	}
}

func pingAll(targets []target, timeout time.Duration) []string {
	statuses := make([]string, len(targets))
	var wg sync.WaitGroup
	wg.Add(len(targets))
	for i := range targets {
		i := i
		go func() {
			defer wg.Done()
			dur, err := tcpPing(targets[i].hostport, timeout)
			if err != nil {
				statuses[i] = fmt.Sprintf("%s %s", labelFor(targets[i]), classifyErr(err))
				return
			}
			statuses[i] = fmt.Sprintf("%s %s", labelFor(targets[i]), formatDuration(dur))
		}()
	}
	wg.Wait()
	return statuses
}

func labelFor(t target) string {
	// Keep the label short: show the raw input if it already includes a scheme,
	// otherwise show host:port (which may include a port we added).
	if strings.Contains(t.raw, "://") {
		return t.raw
	}
	return t.hostport
}

func formatDuration(d time.Duration) string {
	// Round to the nearest millisecond for readability
	if d < time.Millisecond {
		return d.Truncate(time.Microsecond).String()
	}
	return d.Truncate(time.Millisecond).String()
}

func classifyErr(err error) string {
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		return "TIMEOUT"
	}
	// Connection refused is a valid reachability signal but still an error for TCP ping
	// Keep it simple and report as ERR
	return "ERR"
}

func tcpPing(addr string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

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
		// If no explicit port in host, use scheme default or fallback
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
