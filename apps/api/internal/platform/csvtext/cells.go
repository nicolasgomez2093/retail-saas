package csvtext

import "strings"

// Escape preserves text cells that spreadsheet programs might interpret.
func Escape(value string) string {
	if strings.HasPrefix(value, "'") || needsProtection(value) {
		return "'" + value
	}
	return value
}

func Unescape(value string) string {
	if inner, ok := strings.CutPrefix(value, "'"); ok && (strings.HasPrefix(inner, "'") || needsProtection(inner)) {
		return inner
	}
	return value
}

func needsProtection(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") ||
		strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "+") ||
		strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@") ||
		strings.HasPrefix(trimmed, "＝") || strings.HasPrefix(trimmed, "＋") ||
		strings.HasPrefix(trimmed, "－") || strings.HasPrefix(trimmed, "＠") {
		return true
	}
	if len(value) > 1 && value[0] == '0' {
		for _, character := range value {
			if character < '0' || character > '9' {
				return false
			}
		}
		return true
	}
	return false
}
