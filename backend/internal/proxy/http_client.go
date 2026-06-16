package proxy

import (
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

func newHTTPClient(policy Policy, timeout time.Duration) (*http.Client, error) {
	proxyFunc, err := policy.ProxyFunc()
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	return &http.Client{
		Transport: newHTTPTransport(proxyFunc),
		Jar:       jar,
		Timeout:   timeout,
	}, nil
}

func newHTTPTransport(proxyFunc func(*http.Request) (*url.URL, error)) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy:                 proxyFunc,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
