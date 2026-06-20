package main

import (
	"fmt"
	"strings"
)

func hashString(value string) uint32 {
	var hash uint32 = 2166136261
	for _, char := range []byte(value) {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return hash
}

func avgPositive(values []int) int {
	var sum int64
	var count int64
	for _, value := range values {
		if value >= 0 {
			sum += int64(value)
			count++
		}
	}
	if count == 0 {
		return ErrUnknown
	}
	return int(sum / count)
}

func padLeft(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return strings.Repeat(" ", width-len(value)) + value
}

func formatInt(value int64) string {
	return formatInt64(value)
}

func formatInt64(value int64) string {
	negative := value < 0
	if negative {
		value = -value
	}
	raw := fmt.Sprintf("%d", value)
	if len(raw) <= 3 {
		if negative {
			return "-" + raw
		}
		return raw
	}

	var out []byte
	prefix := len(raw) % 3
	if prefix == 0 {
		prefix = 3
	}
	out = append(out, raw[:prefix]...)
	for index := prefix; index < len(raw); index += 3 {
		out = append(out, ',')
		out = append(out, raw[index:index+3]...)
	}
	if negative {
		return "-" + string(out)
	}
	return string(out)
}

func humanBytes(value int64) string {
	return humanBytesFloat(float64(value))
}

func humanBytesFloat(value float64) string {
	if value < 0 {
		return "-"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%s %s", formatInt64(int64(value)), units[index])
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
