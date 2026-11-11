package audience

import (
	"math"
	"strconv"
	"strings"
)

// A model's JSON drifts: a number arrives as a string, a list as one string,
// a field under another spelling. These read what is there and nothing else.

func strOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch x := m[k].(type) {
		case string:
			if t := strings.TrimSpace(x); t != "" {
				return t
			}
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
	}
	return ""
}

func floatOf(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch x := m[k].(type) {
		case float64:
			if !math.IsNaN(x) && !math.IsInf(x, 0) {
				return x, true
			}
		case string:
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(x), "$€£₺"))
			t = strings.ReplaceAll(t, ",", ".")
			if f, err := strconv.ParseFloat(t, 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func intOf(m map[string]any, keys ...string) (int, bool) {
	f, ok := floatOf(m, keys...)
	return int(math.Round(f)), ok
}

func boolOf(m map[string]any, keys ...string) (bool, bool) {
	for _, k := range keys {
		switch x := m[k].(type) {
		case bool:
			return x, true
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "evet":
				return true, true
			case "false", "no", "hayır", "hayir":
				return false, true
			}
		}
	}
	return false, false
}

// listOf reads a list of short texts; a single string is a list of one.
func listOf(m map[string]any, max, maxLen int, keys ...string) []string {
	var out []string
	add := func(s string) {
		s = clip(strings.TrimSpace(s), maxLen)
		if s != "" && len(out) < max {
			out = append(out, s)
		}
	}
	for _, k := range keys {
		switch x := m[k].(type) {
		case []any:
			for _, v := range x {
				switch s := v.(type) {
				case string:
					add(s)
				case map[string]any:
					add(strOf(s, "text", "name", "label", "title"))
				}
			}
		case string:
			add(x)
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

func objectsOf(m map[string]any, keys ...string) []map[string]any {
	for _, k := range keys {
		if list, ok := m[k].([]any); ok {
			out := make([]map[string]any, 0, len(list))
			for _, v := range list {
				if o, ok := v.(map[string]any); ok {
					out = append(out, o)
				}
			}
			return out
		}
	}
	return nil
}
