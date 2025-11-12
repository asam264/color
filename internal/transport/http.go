package transport

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

type HTTPTransport struct {
	timeout   time.Duration
	transport *http.Transport
	once      sync.Once
}

func NewHTTPTransport(timeout time.Duration) *HTTPTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{timeout: timeout}
}

// getTransport 获取共享的 http.Transport 实例，配置连接池参数
func (t *HTTPTransport) getTransport() *http.Transport {
	t.once.Do(func() {
		t.transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: t.timeout,
			DisableKeepAlives:     false,
			ForceAttemptHTTP2:     false,
		}
	})
	return t.transport
}

func (t *HTTPTransport) Proxy(ctx context.Context, target string, req *http.Request, w http.ResponseWriter) error {
	targetURL, err := url.Parse(target)
	if err != nil {
		return err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// 保存原始路径
	origPath := req.URL.Path
	origDirector := proxy.Director

	proxy.Director = func(r *http.Request) {
		origDirector(r)
		r.URL.Path = singleJoiningSlash(targetURL.Path, origPath)
		r.Host = targetURL.Host
		// 使用传入的 context，确保超时控制
		*r = *r.WithContext(ctx)
	}

	// 使用共享的 Transport，支持连接复用
	proxy.Transport = t.getTransport()

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("proxy error: " + e.Error()))
	}

	// 使用 context 控制超时
	proxy.ServeHTTP(w, req.WithContext(ctx))
	return nil
}

// Close 关闭 Transport 并清理所有空闲连接
func (t *HTTPTransport) Close() error {
	if t.transport != nil {
		t.transport.CloseIdleConnections()
	}
	return nil
}

func singleJoiningSlash(a, b string) string {
	aslash := len(a) > 0 && a[len(a)-1] == '/'
	bslash := len(b) > 0 && b[0] == '/'
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
