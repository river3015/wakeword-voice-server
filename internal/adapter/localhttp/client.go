// Package localhttp はループバックのローカルサーバー専用の HTTP クライアントを作る。
// プロキシやリダイレクトで発話内容が外部へ送られないようにする。
package localhttp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ParseBaseURL は http://127.0.0.1:port の形だけを受け付ける。
func ParseBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q はローカルHTTPアドレスではありません", raw)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("%q はローカルHTTPアドレスではありません", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func NewClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("ローカルAPIのリダイレクトは許可しません")
		},
	}
}
