package identity

import "strings"

// DeviceLabel returns only coarse allowlisted names. Never persist a UA fragment,
// version, model, or other client-provided text in the device history.
func DeviceLabel(userAgent string) string {
	ua := strings.ToLower(userAgent)
	browser := ""
	switch {
	case strings.Contains(ua, "zalo"):
		browser = "Zalo in-app"
	case strings.Contains(ua, "fban/") || strings.Contains(ua, "fbav/"):
		browser = "Facebook in-app"
	case strings.Contains(ua, "edg/") || strings.Contains(ua, "edga/") || strings.Contains(ua, "edgios/"):
		browser = "Edge"
	case strings.Contains(ua, "opr/") || strings.Contains(ua, "opera/"):
		browser = "Opera"
	case strings.Contains(ua, "firefox/") || strings.Contains(ua, "fxios/"):
		browser = "Firefox"
	case strings.Contains(ua, "chrome/") || strings.Contains(ua, "crios/"):
		browser = "Chrome"
	case strings.Contains(ua, "safari/") && strings.Contains(ua, "version/"):
		browser = "Safari"
	default:
		return "Trình duyệt không xác định"
	}
	os := ""
	switch {
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod"):
		os = "iOS"
	case strings.Contains(ua, "android"):
		os = "Android"
	case strings.Contains(ua, "windows"):
		os = "Windows"
	case strings.Contains(ua, "cros"):
		os = "ChromeOS"
	case strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os x"):
		os = "macOS"
	case strings.Contains(ua, "linux"):
		os = "Linux"
	}
	if os == "" {
		return browser
	}
	return browser + " · " + os
}
