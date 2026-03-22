package imagegen

import "testing"

func TestResolveOpenAIProxyConfig_UsesDedicatedEnvOverrides(t *testing.T) {
	got := resolveOpenAIProxyConfig(map[string]string{
		"OPENAI_HTTP_PROXY":  "  http://env-http:8080  ",
		"OPENAI_HTTPS_PROXY": "  http://env-https:8080  ",
		"OPENAI_ALL_PROXY":   "  socks5://env-all:1080  ",
		"OPENAI_NO_PROXY":    "  open.feishu.cn,.example.com  ",
	})

	if got.HTTPProxy != "http://env-http:8080" {
		t.Fatalf("unexpected http proxy: %q", got.HTTPProxy)
	}
	if got.HTTPSProxy != "http://env-https:8080" {
		t.Fatalf("unexpected https proxy: %q", got.HTTPSProxy)
	}
	if got.ALLProxy != "socks5://env-all:1080" {
		t.Fatalf("unexpected all proxy: %q", got.ALLProxy)
	}
	if got.NoProxy != "open.feishu.cn,.example.com" {
		t.Fatalf("unexpected no proxy: %q", got.NoProxy)
	}
}

func TestResolveOpenAIProxyConfig_EmptyWhenEnvIsEmpty(t *testing.T) {
	got := resolveOpenAIProxyConfig(map[string]string{
		"OPENAI_HTTPS_PROXY": "   ",
	})

	if got != (openAIProxyConfig{}) {
		t.Fatalf("unexpected proxy config: %#v", got)
	}
}
