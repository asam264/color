package transport

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type HTTPTransport struct {
	timeout time.Duration
}

func NewHTTPTransport(timeout time.Duration) *HTTPTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{timeout: timeout}
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
	}

	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// 可以后续扩展更多配置
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("proxy error: " + e.Error()))
	}

	proxy.ServeHTTP(w, req)
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
