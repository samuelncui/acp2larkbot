package lark

import "testing"

func TestTextContentEscapesJSON(t *testing.T) {
	content, err := textContent("hello \"world\"")
	if err != nil {
		t.Fatalf("textContent failed: %v", err)
	}
	if content != `{"text":"hello \"world\""}` {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestParseTextFallsBackToRawContent(t *testing.T) {
	if got := parseText(`{"text":"hello"}`); got != "hello" {
		t.Fatalf("parseText() = %q", got)
	}
	if got := parseText(`not-json`); got != "not-json" {
		t.Fatalf("parseText fallback = %q", got)
	}
}

func TestLarkDomainNormalization(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		wantOpenURL string
		wantWSDomain string
		wantOpenOK  bool
		wantWSOK    bool
	}{
		{name: "empty", domain: "", wantOpenOK: false, wantWSOK: false},
		{name: "feishu alias", domain: "feishu", wantOpenURL: "https://open.feishu.cn", wantWSDomain: "https://open.feishu.cn", wantOpenOK: true, wantWSOK: true},
		{name: "lark alias", domain: "lark", wantOpenURL: "https://open.larksuite.com", wantWSDomain: "https://open.larksuite.com", wantOpenOK: true, wantWSOK: true},
		{name: "full url", domain: "https://open.larksuite.com", wantOpenURL: "https://open.larksuite.com", wantWSDomain: "https://open.larksuite.com", wantOpenOK: true, wantWSOK: true},
		{name: "host only", domain: "open.feishu.cn", wantOpenURL: "https://open.feishu.cn", wantWSDomain: "https://open.feishu.cn", wantOpenOK: true, wantWSOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOpenURL, gotOpenOK := larkOpenBaseURL(tt.domain)
			if gotOpenOK != tt.wantOpenOK || gotOpenURL != tt.wantOpenURL {
				t.Fatalf("larkOpenBaseURL(%q) = (%q, %v), want (%q, %v)", tt.domain, gotOpenURL, gotOpenOK, tt.wantOpenURL, tt.wantOpenOK)
			}
			gotWSDomain, gotWSOK := larkWSDomain(tt.domain)
			if gotWSOK != tt.wantWSOK || gotWSDomain != tt.wantWSDomain {
				t.Fatalf("larkWSDomain(%q) = (%q, %v), want (%q, %v)", tt.domain, gotWSDomain, gotWSOK, tt.wantWSDomain, tt.wantWSOK)
			}
		})
	}
}
