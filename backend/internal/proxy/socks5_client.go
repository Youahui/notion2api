package proxy

import "net/url"

func IsSOCKS5URL(proxyURL *url.URL) bool {
	return proxyURL != nil && proxyURL.Scheme == ModeSOCKS5
}
