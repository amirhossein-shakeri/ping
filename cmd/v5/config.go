package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Interval     time.Duration
	Timeout      time.Duration
	BufferSize   int
	Refresh      time.Duration
	TopNRows     int
	NoColor      bool
	LogFile      string
	TargetsFile  string
	Methods      []Method
	Schemes      []Scheme
	DefaultPath  string
	InsecureTLS  bool
	UserAgent    string
	MaxBodyBytes int64
	ShowHelp     bool
}

func defaultConfig() Config {
	return Config{
		Interval:     2 * time.Second,
		Timeout:      5 * time.Second,
		BufferSize:   8,
		Refresh:      250 * time.Millisecond,
		TopNRows:     0,
		NoColor:      false,
		DefaultPath:  "/",
		InsecureTLS:  false,
		UserAgent:    "ping-v5/1.0",
		MaxBodyBytes: 32 * 1024,
	}
}

func ParseConfig(args []string) (Config, error) {
	cfg := defaultConfig()

	fs := flag.NewFlagSet("v5", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	methods := fs.String("methods", "GET,HEAD,OPTIONS", "comma-separated HTTP methods")
	schemes := fs.String("schemes", "HTTP,HTTPS", "comma-separated schemes")

	fs.DurationVar(&cfg.Interval, "interval", cfg.Interval, "request interval per row")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "request timeout")
	fs.DurationVar(&cfg.Refresh, "refresh", cfg.Refresh, "screen refresh interval")
	fs.IntVar(&cfg.BufferSize, "buffer", cfg.BufferSize, "rolling buffer size per row")
	fs.IntVar(&cfg.TopNRows, "max-rows", cfg.TopNRows, "limit rendered rows to fit smaller screens, 0 keeps all")
	fs.BoolVar(&cfg.NoColor, "no-color", cfg.NoColor, "disable ANSI colors")
	fs.BoolVar(&cfg.InsecureTLS, "insecure-tls", cfg.InsecureTLS, "skip TLS verification")
	fs.StringVar(&cfg.LogFile, "log-file", "", "append JSONL probe results to file")
	fs.StringVar(&cfg.TargetsFile, "targets-file", "", "optional JSON file with targets")
	fs.StringVar(&cfg.DefaultPath, "path", cfg.DefaultPath, "default request path for targets without explicit path")
	fs.StringVar(&cfg.UserAgent, "user-agent", cfg.UserAgent, "HTTP user agent")
	fs.Int64Var(&cfg.MaxBodyBytes, "max-body-bytes", cfg.MaxBodyBytes, "cap read bytes per response for traffic accounting")
	fs.BoolVar(&cfg.ShowHelp, "help", false, "show help")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.ShowHelp {
		fs.Usage()
		return cfg, flag.ErrHelp
	}
	if cfg.BufferSize < 1 {
		return cfg, fmt.Errorf("buffer must be >= 1")
	}
	if cfg.Interval <= 0 || cfg.Timeout <= 0 || cfg.Refresh <= 0 {
		return cfg, fmt.Errorf("durations must be > 0")
	}
	if cfg.MaxBodyBytes < 0 {
		return cfg, fmt.Errorf("max-body-bytes must be >= 0")
	}

	parsedMethods, err := parseMethods(*methods)
	if err != nil {
		return cfg, err
	}
	parsedSchemes, err := parseSchemes(*schemes)
	if err != nil {
		return cfg, err
	}
	cfg.Methods = parsedMethods
	cfg.Schemes = parsedSchemes

	if cfg.TargetsFile != "" {
		cfg.TargetsFile = filepath.Clean(cfg.TargetsFile)
	}
	if cfg.LogFile != "" {
		cfg.LogFile = filepath.Clean(cfg.LogFile)
	}

	return cfg, nil
}

type Target struct {
	Title   string   `json:"title"`
	Address string   `json:"address"`
	Schemes []Scheme `json:"schemes"`
	Methods []Method `json:"methods"`
	Path    string   `json:"path"`
}

func loadTargets(cfg Config) ([]Target, error) {
	if cfg.TargetsFile == "" {
		return defaultTargets(cfg), nil
	}

	raw, err := os.ReadFile(cfg.TargetsFile)
	if err != nil {
		return nil, err
	}

	var targets []Target
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil, err
	}

	for index := range targets {
		normalizeTarget(&targets[index], cfg)
	}

	return targets, nil
}

func normalizeTarget(target *Target, cfg Config) {
	target.Title = strings.TrimSpace(target.Title)
	target.Address = strings.TrimSpace(target.Address)
	if target.Path == "" {
		target.Path = cfg.DefaultPath
	}
	if !strings.HasPrefix(target.Path, "/") {
		target.Path = "/" + target.Path
	}
	if len(target.Schemes) == 0 {
		target.Schemes = append([]Scheme(nil), cfg.Schemes...)
	}
	if len(target.Methods) == 0 {
		target.Methods = append([]Method(nil), cfg.Methods...)
	}
}

func defaultTargets(cfg Config) []Target {
	targets := []Target{
		{Title: "Cloudflare Trace", Address: "cloudflare.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}, Path: "/cdn-cgi/trace"},
		{Title: "GitHub", Address: "github.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodHead}},
		{Title: "GStatic", Address: "connectivitycheck.gstatic.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}, Path: "/generate_204"},
		{Title: "go.dev", Address: "go.dev", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodHead}},
		{Title: "Microsoft", Address: "msftconnecttest.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}, Path: "/connecttest.txt"},
		{Title: "Firefox", Address: "detectportal.firefox.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}, Path: "/success.txt"},

		// Demo
		// {Title: "Cloudflare", Address: "cloudflare.com"},
		// {Title: "Google", Address: "google.com"},
		// {Title: "GitHub", Address: "github.com"},
		// {Title: "1.1.1.1", Address: "1.1.1.1"},
		// {Title: "8.8.8.8", Address: "8.8.8.8"},
		// {Title: "4.2.2.4", Address: "4.2.2.4"},
		// {Title: "example.com", Address: "example.com"},
		// {Title: "Firefox", Address: "detectportal.firefox.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}},
		// {Title: "GStatic", Address: "www.gstatic.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodHead}},
		// {Title: "Cloudflare Trace", Address: "cloudflare.com", Schemes: []Scheme{SchemeHTTP}, Methods: []Method{MethodOptions}, Path: "/cdn-cgi/trace"},
		// {Title: "Microsoft", Address: "microsoft.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}},
		// {Title: "YouTube", Address: "youtube.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}},
		// {Title: "LinkedIn", Address: "linkedin.com", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodHead}},
		// {Title: "go.dev", Address: "go.dev", Schemes: []Scheme{SchemeHTTPS}, Methods: []Method{MethodGet}},
		// {Title: "google.cn", Address: "google.cn", Schemes: []Scheme{SchemeHTTP}, Methods: []Method{MethodOptions}},
	}

	for index := range targets {
		normalizeTarget(&targets[index], cfg)
	}

	return targets
}

func parseMethods(raw string) ([]Method, error) {
	parts := splitCSV(raw)
	out := make([]Method, 0, len(parts))
	seen := map[Method]struct{}{}

	for _, part := range parts {
		method := Method(strings.ToUpper(part))
		switch method {
		case MethodGet, MethodHead, MethodOptions:
		default:
			return nil, fmt.Errorf("unsupported method %q", part)
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		out = append(out, method)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no methods configured")
	}
	return out, nil
}

func parseSchemes(raw string) ([]Scheme, error) {
	parts := splitCSV(raw)
	out := make([]Scheme, 0, len(parts))
	seen := map[Scheme]struct{}{}

	for _, part := range parts {
		scheme := Scheme(strings.ToUpper(part))
		switch scheme {
		case SchemeHTTP, SchemeHTTPS:
		default:
			return nil, fmt.Errorf("unsupported scheme %q", part)
		}
		if _, ok := seen[scheme]; ok {
			continue
		}
		seen[scheme] = struct{}{}
		out = append(out, scheme)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no schemes configured")
	}
	return out, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
