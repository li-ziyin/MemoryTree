package mcpserver

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExcerptBodyAroundMatch(t *testing.T) {
	body := strings.Repeat("填充内容。", 300) + "紫色风铃只出现一次。" + strings.Repeat("尾部内容。", 300)
	got, matched := excerptBody(body, "紫色风铃", 300)
	if !matched {
		t.Fatalf("match reported false")
	}
	if !strings.Contains(got, "紫色风铃") {
		t.Errorf("excerpt does not contain the match: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 300 {
		t.Errorf("excerpt is %d runes, want <= 300", n)
	}
	if n := utf8.RuneCountInString(got); n >= utf8.RuneCountInString(body) {
		t.Errorf("excerpt was not shortened: %d runes", n)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt should be clipped at both ends: %q", got)
	}
}

func TestExcerptBodyNoMatch(t *testing.T) {
	body := strings.Repeat("填充内容。", 300)
	got, matched := excerptBody(body, "不存在的词", 200)
	if matched {
		t.Fatalf("match reported true")
	}
	if n := utf8.RuneCountInString(got); n > 200 {
		t.Errorf("fallback is %d runes, want <= 200", n)
	}
	if !strings.HasPrefix(got, "填充内容。") {
		t.Errorf("fallback should start at the beginning: %q", got)
	}
}

func TestExcerptBodyShortBody(t *testing.T) {
	got, matched := excerptBody("很短", "短", 200)
	if got != "很短" || !matched {
		t.Errorf("excerptBody = %q, %v; want full body and true", got, matched)
	}
}

func TestExcerptBodyMultipleMatches(t *testing.T) {
	body := strings.Repeat("甲", 500) + "关键词一" + strings.Repeat("乙", 1000) +
		"关键词二" + strings.Repeat("丙", 500)
	got, matched := excerptBody(body, "关键词一 关键词二", 600)
	if !matched {
		t.Fatalf("match reported false")
	}
	if !strings.Contains(got, "关键词一") || !strings.Contains(got, "关键词二") {
		t.Errorf("excerpt misses a match: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 600 {
		t.Errorf("excerpt is %d runes, want <= 600", n)
	}
}
