package main

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"
)

func NewApp(cfg Config) (*App, error) {
	targets, err := loadTargets(cfg)
	if err != nil {
		return nil, err
	}

	proxyURL := detectProxy()
	withProxy := proxyURL != nil
	rows := buildRows(targets, cfg.BufferSize, withProxy)
	sortRows(rows)

	logger, err := NewResultLogger(cfg.LogFile)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:        cfg,
		startedAt:  time.Now(),
		proxyURL:   proxyURL,
		withProxy:  withProxy,
		rows:       rows,
		directHTTP: NewHTTPClient(nil, cfg),
		proxyHTTP:  nil,
		snapshots:  NewHistoryStore(withProxy),
		logger:     logger,
	}
	if withProxy {
		app.proxyHTTP = NewHTTPClient(proxyURL, cfg)
	}
	return app, nil
}

func buildRows(targets []Target, bufferSize int, withProxy bool) []*RowState {
	rows := make([]*RowState, 0, len(targets)*4)
	for _, target := range targets {
		for _, scheme := range target.Schemes {
			for _, method := range target.Methods {
				rows = append(rows, NewRowState(target, scheme, method, bufferSize, withProxy))
			}
		}
	}
	return rows
}

func (app *App) Run(ctx context.Context) error {
	defer app.logger.Close()

	hideCursor()
	defer showCursor()

	for _, row := range app.rows {
		go app.rowWorker(ctx, row, ModeDirect, app.directHTTP)
		if app.withProxy {
			go app.rowWorker(ctx, row, ModeProxy, app.proxyHTTP)
		}
	}

	refresh := time.NewTicker(app.cfg.Refresh)
	defer refresh.Stop()
	uptime := time.NewTicker(time.Second)
	defer uptime.Stop()

	for {
		select {
		case <-ctx.Done():
			clearScreen()
			_, _ = os.Stdout.WriteString(app.Render(time.Now()))
			return nil
		case <-uptime.C:
		case <-refresh.C:
		}

		clearScreen()
		if _, err := os.Stdout.WriteString(app.Render(time.Now())); err != nil && !errors.Is(err, os.ErrClosed) {
			return err
		}
	}
}

func (app *App) rowWorker(ctx context.Context, row *RowState, mode Mode, client *SafeHTTPClient) {
	if client == nil {
		return
	}

	time.Sleep(time.Duration(hashString(row.Title+row.Address+string(row.Scheme)+string(row.Method)+string(mode))%1000) * time.Millisecond)

	ticker := time.NewTicker(app.cfg.Interval)
	defer ticker.Stop()

	for {
		app.runProbe(ctx, row, mode, client)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (app *App) runProbe(ctx context.Context, row *RowState, mode Mode, client *SafeHTTPClient) {
	row.mu.Lock()
	state := row.modeState(mode)
	if state == nil {
		row.mu.Unlock()
		return
	}
	state.Running = true
	row.mu.Unlock()

	result := client.Probe(ctx, row, mode)

	row.mu.Lock()
	state = row.modeState(mode)
	state.Running = false
	state.Buffer.Add(result.PingMS)
	state.Latest = result
	state.Count++
	state.Traffic += result.TotalBytes
	if result.PingMS >= 0 {
		state.Success++
	} else {
		state.Failed++
	}
	row.mu.Unlock()

	app.snapshots.Add(result)
	_ = app.logger.Write(result)
}

func sortRows(rows []*RowState) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		if left.Title != right.Title {
			return left.Title < right.Title
		}
		if left.Address != right.Address {
			return left.Address < right.Address
		}
		if left.Scheme != right.Scheme {
			return left.Scheme < right.Scheme
		}
		return left.Method < right.Method
	})
}
