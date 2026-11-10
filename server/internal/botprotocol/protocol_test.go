package botprotocol

import (
	"testing"
	"unicode/utf8"
)

func TestDingTalkSessionWebhookValidation(t *testing.T) {
	for _, raw := range []string{
		"https://oapi.dingtalk.com/robot/sendBySession?session=opaque-token",
		"https://oapi.dingtalk.com:443/robot/sendBySession?session=opaque-token",
	} {
		if err := ValidateDingTalkWebhook(raw); err != nil {
			t.Fatalf("official endpoint rejected: %v", err)
		}
	}
	for _, raw := range []string{
		"", "http://oapi.dingtalk.com/robot/sendBySession?session=x",
		"https://api.dingtalk.com/robot/sendBySession?session=x",
		"https://oapi.dingtalk.com.evil.test/robot/sendBySession?session=x",
		"https://oapi.dingtalk.com@127.0.0.1/robot/sendBySession?session=x",
		"https://user:pass@oapi.dingtalk.com/robot/sendBySession?session=x",
		"https://127.0.0.1/robot/sendBySession?session=x",
		"https://[::1]/robot/sendBySession?session=x",
		"https://oapi.dingtalk.com:8443/robot/sendBySession?session=x",
		"https://oapi.dingtalk.com/robot/send?session=x",
		"https://oapi.dingtalk.com/robot/sendBySession/?session=x",
		"https://oapi.dingtalk.com/robot/%73endBySession?session=x",
		"https://oapi.dingtalk.com/robot/sendBySession?session=x#fragment",
		"https://oapi.dingtalk.com/robot/sendBySession?session=x#",
		"https://oapi.dingtalk.com/robot/sendBySession",
		"https://oapi.dingtalk.com/robot/sendBySession?session=",
		"https://oapi.dingtalk.com/robot/sendBySession?session=%20",
		"https://oapi.dingtalk.com/robot/sendBySession?session=a&session=b",
		"https://oapi.dingtalk.com/robot/sendBySession?session=%ZZ",
		" https://oapi.dingtalk.com/robot/sendBySession?session=x",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateDingTalkWebhook(raw); err == nil {
				t.Fatal("unsafe/non-session endpoint accepted")
			}
		})
	}
}

func TestClipUTF8(t *testing.T) {
	for _, tc := range []struct {
		s    string
		max  int
		want string
	}{
		{"abc", 0, ""}, {"abc", -1, ""}, {"abc", 2, "ab"}, {"abc", 5, "abc"},
		{"你好🙂ok", 1, ""}, {"你好🙂ok", 3, "你"}, {"你好🙂ok", 8, "你好"},
		{"你好🙂ok", 10, "你好🙂"}, {"a\xffb", 4, "a\ufffd"},
	} {
		got := ClipUTF8(tc.s, tc.max)
		if got != tc.want || !utf8.ValidString(got) || (tc.max >= 0 && len(got) > tc.max) {
			t.Errorf("ClipUTF8(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
		}
	}
}
