// internal/service/transport.go
// 農產品價差雷達系統 — HTTP 傳輸層抽象
// 負責：提供可替換的 HTTP Transport 介面，預留未來 Proxy 擴充點
// 目前實作 DirectTransport（直接連線），未來可新增 ProxyTransport

package service

import (
	"net"
	"net/http"
	"time"
)

// NewDirectTransport 建立直接連線的 HTTP Transport（預設）
// 設定合理的連線池與超時參數
func NewDirectTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

// 未來擴充：
// func NewProxyTransport(proxyURL string) (*http.Transport, error) { ... }
// func NewRotatingProxyTransport(proxyURLs []string) (*http.Transport, error) { ... }
