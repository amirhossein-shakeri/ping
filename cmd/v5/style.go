package main

const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
	ansiDim    = "\033[2m"
)

func colorize(value string, color string, noColor bool) string {
	if noColor {
		return value
	}
	return color + value + ansiReset
}

func dim(value string, noColor bool) string {
	return colorize(value, ansiDim, noColor)
}
