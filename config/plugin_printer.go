package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
)

var headers = []string{"Name", "Source(s)"}

type pluginPrinter struct {
	name   string
	source string
}

type pluginNames []pluginPrinter

func GetPluginSourcesTable(pluginNames []pluginPrinter) string {
	if !PrintPluginConfigSource {
		return ""
	}

	data := make([][]any, 0, len(pluginNames))
	rows := make(map[string][]string)
	for _, plugin := range pluginNames {
		if _, ok := rows[plugin.name]; !ok {
			rows[plugin.name] = make([]string, 0)
		}
		rows[plugin.name] = append(rows[plugin.name], plugin.source)
	}

	for name, sources := range rows {
		var nameCountStr string
		if len(sources) > 1 {
			nameCountStr = fmt.Sprintf("%s (%dx)", name, len(sources))
		} else {
			nameCountStr = name
		}
		data = append(data, []any{nameCountStr, sources})
	}
	sort.Slice(data, func(i, j int) bool {
		return len(data[i][1].([]string)) > len(data[j][1].([]string))
	})
	return getTableString(headers, data)
}

func getTableString(headers []string, data [][]any) string {
	buff := new(bytes.Buffer)

	t := table.NewWriter()
	t.SetOutputMirror(buff)
	t.AppendHeader(convertToRow(headers))

	// Append rows
	for _, row := range data {
		processedRow := make([]interface{}, len(row))
		for i, col := range row {
			switch v := col.(type) {
			case []string: // Convert slices to multi-line strings
				processedRow[i] = strings.Join(v, "\n")
			default:
				processedRow[i] = v
			}
		}
		t.AppendRow(processedRow)
	}

	t.Style().Options.SeparateRows = true
	return t.Render()
}

// Helper function to convert headers to table.Row
func convertToRow(data []string) table.Row {
	row := make(table.Row, len(data))
	for i, val := range data {
		row[i] = val
	}
	return row
}
