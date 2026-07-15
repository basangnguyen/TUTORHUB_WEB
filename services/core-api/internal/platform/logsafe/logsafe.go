package logsafe

import "strings"

// String removes line separators that could forge additional plain-text log entries.
func String(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\u2028", "")
	return strings.ReplaceAll(value, "\u2029", "")
}

func Error(err error) string {
	if err == nil {
		return ""
	}

	return String(err.Error())
}
