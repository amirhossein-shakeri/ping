package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func clearScreen() {
	_, _ = os.Stdout.WriteString("\033[H\033[2J")
}

func hideCursor() {
	_, _ = os.Stdout.WriteString("\033[?25l")
}

func showCursor() {
	_, _ = os.Stdout.WriteString("\033[?25h")
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func visibleLen(value string) int {
	return len([]rune(stripANSI(value)))
}

func stripANSI(value string) string {
	var out strings.Builder
	inEscape := false
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if char >= '@' && char <= '~' {
				inEscape = false
			}
			continue
		}
		out.WriteByte(char)
	}
	return out.String()
}

func truncateANSI(value string, width int) string {
	if visibleLen(value) <= width {
		return value
	}

	plain := []rune(stripANSI(value))
	if width <= 1 {
		return string(plain[:max(0, width)])
	}
	return string(plain[:width-1]) + "…"
}
