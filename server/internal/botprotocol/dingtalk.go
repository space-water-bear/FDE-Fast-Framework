package botprotocol

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// ValidateDingTalkWebhook permits only the documented temporary reply endpoint.
// The HTTP client must additionally disable redirects before using this URL.
func ValidateDingTalkWebhook(raw string) error {
	invalid := errors.New("invalid DingTalk session webhook")
	u, err := url.Parse(raw)
	if err != nil || u == nil || strings.TrimSpace(raw) != raw || u.Opaque != "" ||
		u.Scheme != "https" || u.User != nil || strings.Contains(raw, "#") ||
		(!strings.EqualFold(u.Host, "oapi.dingtalk.com") && !strings.EqualFold(u.Host, "oapi.dingtalk.com:443")) ||
		u.Path != "/robot/sendBySession" || u.EscapedPath() != "/robot/sendBySession" {
		return invalid
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["session"]) != 1 || strings.TrimSpace(query.Get("session")) == "" {
		return invalid
	}
	return nil
}

// ClipUTF8 truncates text to a byte budget without splitting a Unicode code point.
func ClipUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\ufffd")
	}
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
