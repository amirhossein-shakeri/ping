package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func (app *App) Render(now time.Time) string {
	width, height := terminalSize()
	_ = height

	numberWidth := app.numberWidth()
	minPing, maxPing := app.globalPingRange()

	var out strings.Builder
	out.WriteString("Async HTTP Reachability Table\n\n")
	out.WriteString(proxyBanner(app.proxyURL))
	out.WriteString("Press Ctrl+C to stop.\n\n")

	out.WriteString(app.topBar(now))
	out.WriteByte('\n')
	out.WriteString(app.trafficBar(now))
	out.WriteByte('\n')

	table := app.tableModel(width, numberWidth, minPing, maxPing)
	out.WriteString(table)
	out.WriteByte('\n')
	out.WriteString(app.bottomBar(now))
	out.WriteByte('\n')
	return out.String()
}

func proxyBanner(proxyURL *url.URL) string {
	if proxyURL == nil {
		return "Proxy detected: none\n"
	}
	return "Proxy detected: " + proxyURL.Redacted() + "\n"
}

func (app *App) topBar(now time.Time) string {
	direct := app.aggregateMode(ModeDirect)
	proxy := app.aggregateMode(ModeProxy)

	parts := []string{
		"Uptime " + now.Sub(app.startedAt).Round(time.Second).String(),
		"Total requests sent: " + formatInt(direct.Count+proxy.Count),
		"Success(Direct): " + formatMaybeCount(direct.Success),
		"Success(Proxy): " + formatMaybeModeCount(proxy.Success, app.withProxy),
		"Failed(Direct): " + formatMaybeCount(direct.Failed),
		"Failed(Proxy): " + formatMaybeModeCount(proxy.Failed, app.withProxy),
	}
	return barLine(parts)
}

func (app *App) trafficBar(now time.Time) string {
	directTotal := app.modeTrafficTotal(ModeDirect)
	proxyTotal := app.modeTrafficTotal(ModeProxy)
	last5mDirect := app.snapshots.Summary(ModeDirect, 5*time.Minute, now)
	last5mProxy := app.snapshots.Summary(ModeProxy, 5*time.Minute, now)

	parts := []string{
		"Total traffic usage: " + humanBytes(directTotal+proxyTotal),
		"Direct traffic usage: " + humanBytes(directTotal),
		"Proxy traffic usage: " + formatMaybeTraffic(proxyTotal, app.withProxy),
		"Avg traffic/s 5m: " + humanBytesFloat(last5mDirect.AvgTrafficS+last5mProxy.AvgTrafficS) + "/s",
		"Avg req/s 5m: " + fmt.Sprintf("%.2f", last5mDirect.AvgReqPerS+last5mProxy.AvgReqPerS),
	}
	return barLine(parts)
}

func (app *App) bottomBar(now time.Time) string {
	d30 := app.snapshots.Summary(ModeDirect, 30*time.Second, now)
	d5m := app.snapshots.Summary(ModeDirect, 5*time.Minute, now)
	d15m := app.snapshots.Summary(ModeDirect, 15*time.Minute, now)
	d1h := app.snapshots.Summary(ModeDirect, time.Hour, now)

	parts := []string{
		"AVG 30s: " + formatSummaryPing(d30.AvgPing),
		"AVG 5m: " + formatSummaryPing(d5m.AvgPing),
		"AVG 15m: " + formatSummaryPing(d15m.AvgPing),
		"AVG 1h: " + formatSummaryPing(d1h.AvgPing),
	}
	if app.withProxy {
		p30 := app.snapshots.Summary(ModeProxy, 30*time.Second, now)
		p5m := app.snapshots.Summary(ModeProxy, 5*time.Minute, now)
		p15m := app.snapshots.Summary(ModeProxy, 15*time.Minute, now)
		p1h := app.snapshots.Summary(ModeProxy, time.Hour, now)
		parts = append(parts,
			"Proxy AVG 30s: "+formatSummaryPing(p30.AvgPing),
			"Proxy AVG 5m: "+formatSummaryPing(p5m.AvgPing),
			"Proxy AVG 15m: "+formatSummaryPing(p15m.AvgPing),
			"Proxy AVG 1h: "+formatSummaryPing(p1h.AvgPing),
		)
	}
	return barLine(parts)
}

func (app *App) tableModel(width int, numberWidth int, minPing int, maxPing int) string {
	headers := []string{"Title / Address", "Scheme", "Method", "Ping(ms) Buffer"}
	if app.withProxy {
		headers = append(headers, "Proxy Ping(ms) Buffer")
	}
	headers = append(headers, "AVG")
	if app.withProxy {
		headers = append(headers, "Proxy AVG")
	}

	renderRows := app.collectRenderRows(numberWidth, minPing, maxPing)
	table := NewTable(headers)
	for _, row := range renderRows {
		table.Add(row)
	}
	table.MaxWidth = width
	return table.Render()
}

func (app *App) collectRenderRows(numberWidth int, minPing int, maxPing int) [][]string {
	rows := make([][]string, 0, len(app.rows))
	lastTitle := ""

	limit := len(app.rows)
	if app.cfg.TopNRows > 0 && app.cfg.TopNRows < limit {
		limit = app.cfg.TopNRows
	}

	for index := 0; index < limit; index++ {
		row := app.rows[index]
		row.mu.RLock()
		title := row.Title
		if lastTitle == row.Title {
			title = ""
		} else {
			lastTitle = row.Title
		}

		directBuffer := formatBuffer(row.Direct, numberWidth, minPing, maxPing, app.cfg.NoColor)
		directAvg := formatPingFixed(avgPositive(row.Direct.Buffer.Snapshot()), numberWidth, minPing, maxPing, app.cfg.NoColor)

		label := row.Address
		if title != "" {
			label = title + " · " + row.Address
		}
		line := []string{label, string(row.Scheme), string(row.Method), directBuffer}
		if app.withProxy {
			line = append(line, formatBuffer(row.Proxy, numberWidth, minPing, maxPing, app.cfg.NoColor))
		}
		line = append(line, directAvg)
		if app.withProxy {
			line = append(line, formatPingFixed(avgPositive(row.Proxy.Buffer.Snapshot()), numberWidth, minPing, maxPing, app.cfg.NoColor))
		}
		row.mu.RUnlock()
		rows = append(rows, line)
	}
	return rows
}

func (app *App) numberWidth() int {
	width := 2
	for _, row := range app.rows {
		row.mu.RLock()
		for _, value := range row.Direct.Buffer.Snapshot() {
			width = max(width, len(strconv.Itoa(value)))
		}
		if row.Proxy != nil {
			for _, value := range row.Proxy.Buffer.Snapshot() {
				width = max(width, len(strconv.Itoa(value)))
			}
		}
		row.mu.RUnlock()
	}
	return width
}

func (app *App) globalPingRange() (int, int) {
	minPing := int(^uint(0) >> 1)
	maxPing := -1
	for _, row := range app.rows {
		row.mu.RLock()
		for _, value := range row.Direct.Buffer.Snapshot() {
			if value >= 0 {
				if value < minPing {
					minPing = value
				}
				if value > maxPing {
					maxPing = value
				}
			}
		}
		if row.Proxy != nil {
			for _, value := range row.Proxy.Buffer.Snapshot() {
				if value >= 0 {
					if value < minPing {
						minPing = value
					}
					if value > maxPing {
						maxPing = value
					}
				}
			}
		}
		row.mu.RUnlock()
	}
	if maxPing < 0 {
		return 0, 0
	}
	if minPing == int(^uint(0)>>1) {
		minPing = 0
	}
	return minPing, maxPing
}

type modeAggregate struct {
	Count   int64
	Success int64
	Failed  int64
	Traffic int64
}

func (app *App) aggregateMode(mode Mode) modeAggregate {
	var result modeAggregate
	for _, row := range app.rows {
		row.mu.RLock()
		state := row.modeState(mode)
		if state != nil {
			result.Count += state.Count
			result.Success += state.Success
			result.Failed += state.Failed
			result.Traffic += state.Traffic
		}
		row.mu.RUnlock()
	}
	return result
}

func (app *App) modeTrafficTotal(mode Mode) int64 {
	var total int64
	for _, row := range app.rows {
		row.mu.RLock()
		state := row.modeState(mode)
		if state != nil {
			total += app.modeStateTraffic(state)
		}
		row.mu.RUnlock()
	}
	return total
}

func (app *App) modeStateTraffic(state *ModeState) int64 {
	if state == nil {
		return 0
	}
	return state.Traffic
}

func formatBuffer(state *ModeState, numberWidth int, minPing int, maxPing int, noColor bool) string {
	if state == nil {
		return "-"
	}

	values := state.Buffer.Snapshot()
	parts := make([]string, 0, len(values)+1)
	for _, value := range values {
		parts = append(parts, formatPingFixed(value, numberWidth, minPing, maxPing, noColor))
	}
	if state.Running {
		parts = append(parts, dim("…", noColor))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatPingFixed(value int, width int, minPing int, maxPing int, noColor bool) string {
	text := padLeft(formatInt64(int64(value)), width)
	if value < 0 {
		return colorize(text, ansiRed, noColor)
	}
	if noColor {
		return text
	}
	if maxPing <= minPing {
		return colorize(text, ansiGreen, noColor)
	}
	ratio := float64(value-minPing) / float64(maxPing-minPing)
	switch {
	case ratio <= 0.33:
		return colorize(text, ansiGreen, noColor)
	case ratio <= 0.66:
		return colorize(text, ansiYellow, noColor)
	default:
		return colorize(text, ansiRed, noColor)
	}
}

func formatSummaryPing(value int) string {
	if value < 0 {
		return strconv.Itoa(value)
	}
	return formatInt64(int64(value)) + "ms"
}

func formatMaybeCount(value int64) string {
	return formatInt64(value)
}

func formatMaybeModeCount(value int64, enabled bool) string {
	if !enabled {
		return "-"
	}
	return formatInt64(value)
}

func formatMaybeTraffic(value int64, enabled bool) string {
	if !enabled {
		return "-"
	}
	return humanBytes(value)
}

func barLine(parts []string) string {
	return "| " + strings.Join(parts, " . ") + " |"
}

func terminalSize() (int, int) {
	cols := getenvInt("COLUMNS", 160)
	rows := getenvInt("LINES", 40)
	return cols, rows
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
