package imagegen

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
)

func newHTTPClient(proxyCfg openAIProxyConfig, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyFunc, err := buildProxyFunc(proxyCfg)
	if err != nil {
		return nil, err
	}
	transport.Proxy = proxyFunc
	transport.ForceAttemptHTTP2 = true
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func buildProxyFunc(proxyCfg openAIProxyConfig) (func(*http.Request) (*url.URL, error), error) {
	proxyCfg = openAIProxyConfig{
		HTTPProxy:  strings.TrimSpace(proxyCfg.HTTPProxy),
		HTTPSProxy: strings.TrimSpace(proxyCfg.HTTPSProxy),
		ALLProxy:   strings.TrimSpace(proxyCfg.ALLProxy),
		NoProxy:    strings.TrimSpace(proxyCfg.NoProxy),
	}
	if proxyCfg.HTTPProxy == "" {
		proxyCfg.HTTPProxy = proxyCfg.ALLProxy
	}
	if proxyCfg.HTTPSProxy == "" {
		proxyCfg.HTTPSProxy = proxyCfg.ALLProxy
	}
	if proxyCfg.HTTPProxy == "" && proxyCfg.HTTPSProxy == "" && proxyCfg.NoProxy == "" {
		return nil, nil
	}
	cfg := &httpproxy.Config{
		HTTPProxy:  proxyCfg.HTTPProxy,
		HTTPSProxy: proxyCfg.HTTPSProxy,
		NoProxy:    proxyCfg.NoProxy,
	}
	proxyForURL := cfg.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		return proxyForURL(req.URL)
	}, nil
}

func lookupEnv(env map[string]string, key string) string {
	if len(env) == 0 {
		return ""
	}
	return strings.TrimSpace(env[strings.ToUpper(strings.TrimSpace(key))])
}

func compactExistingPaths(paths []string, limit int) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
