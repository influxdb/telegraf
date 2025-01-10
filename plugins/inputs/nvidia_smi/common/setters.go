package common

import (
	"strconv"
	"strings"
)

// SetTagIfUsed sets those tags whose value is different from empty string.
func SetTagIfUsed(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}

// SetIfUsed sets those fields whose value is different from empty string.
func SetIfUsed(t string, m map[string]interface{}, k, v string) {
	vals := strings.Fields(v)
	if len(vals) < 1 {
		return
	}

	val := vals[0]
	if k == "pcie_link_width_current" {
		val = strings.TrimSuffix(vals[0], "x")
	}

	switch t {
	case "float":
		if val != "" {
			f, err := strconv.ParseFloat(val, 64)
			if err == nil {
				m[k] = f
			}
		}
	case "int":
		if val != "" && val != "N/A" {
			i, err := strconv.Atoi(val)
			if err == nil {
				m[k] = i
			}
		}
	case "str":
		if val != "" && val != "N/A" {
			m[k] = val
		}
	}
}
