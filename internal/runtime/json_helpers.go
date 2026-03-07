package runtime

import "fmt"

func asString(m map[string]any, key string, def string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	if s == "" {
		return def
	}
	return s
}

func asBool(m map[string]any, key string, def bool) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	b, ok := v.(bool)
	if ok {
		return b
	}
	return def
}

func asFloat(m map[string]any, key string, def float64) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return def
	}
}

func asMap(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok || v == nil {
		return map[string]any{}
	}
	mv, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return mv
}

func asStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch vv := v.(type) {
	case []string:
		return vv
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		return nil
	}
}
