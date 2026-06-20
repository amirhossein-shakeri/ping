package main

import "strings"

type Table struct {
	Headers  []string
	Rows     [][]string
	Widths   []int
	MaxWidth int
}

func NewTable(headers []string) *Table {
	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = visibleLen(header)
	}
	return &Table{Headers: headers, Widths: widths}
}

func (table *Table) Add(row []string) {
	table.Rows = append(table.Rows, row)
	for index, cell := range row {
		if index >= len(table.Widths) {
			continue
		}
		if width := visibleLen(cell); width > table.Widths[index] {
			table.Widths[index] = width
		}
	}
}

func (table *Table) Render() string {
	table.fitToWidth()
	var out strings.Builder
	out.WriteString(table.separator())
	out.WriteByte('\n')
	out.WriteString(table.renderRow(table.Headers))
	out.WriteByte('\n')
	out.WriteString(table.separator())
	for _, row := range table.Rows {
		out.WriteByte('\n')
		out.WriteString(table.renderRow(row))
	}
	out.WriteByte('\n')
	out.WriteString(table.separator())
	return out.String()
}

func (table *Table) fitToWidth() {
	if table.MaxWidth <= 0 || len(table.Widths) == 0 {
		return
	}
	total := 1
	for _, width := range table.Widths {
		total += width + 3
	}
	if total <= table.MaxWidth {
		return
	}

	for total > table.MaxWidth && len(table.Widths) >= 4 {
		targetIndex := len(table.Widths) - 3
		if table.Widths[targetIndex] <= 16 {
			break
		}
		table.Widths[targetIndex]--
		total--
	}
}

func (table *Table) separator() string {
	var out strings.Builder
	out.WriteByte('|')
	for _, width := range table.Widths {
		out.WriteByte(' ')
		out.WriteString(strings.Repeat("-", width))
		out.WriteString(" |")
	}
	return out.String()
}

func (table *Table) renderRow(row []string) string {
	var out strings.Builder
	out.WriteByte('|')
	for index, cell := range row {
		width := table.Widths[index]
		cell = truncateANSI(cell, width)
		out.WriteByte(' ')
		out.WriteString(cell)
		out.WriteString(strings.Repeat(" ", max(0, width-visibleLen(cell))))
		out.WriteString(" |")
	}
	return out.String()
}
