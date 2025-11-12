package transport

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// HTTPTransport HTTP 传输层实现
// 核心优化：
// 1. 为每个 target URL 缓存 ReverseProxy 实例，避免重复创建导致的连接冲突
// 2. 使用共享的 Transport 连接池，支持连接复用
// 3. 添加详细日志记录，便于调试
type HTTPTransport struct {
	timeout   time.Duration
	transport *http.Transport
	once      sync.Once

	// 缓存每个 target 的 ReverseProxy 实例，避免重复创建
	proxyCache sync.Map // map[string]*cachedProxy

	// 日志开关（可选，未来可扩展为接口）
	enableLog bool
}

type cachedProxy struct {
	proxy   *httputil.ReverseProxy
	target  *url.URL
	mu      sync.RWMutex
	lastUse time.Time
}

func NewHTTPTransport(timeout time.Duration) *HTTPTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{
		timeout:   timeout,
		enableLog: true, // 默认启用日志
	}
}

// getTransport 获取共享的 http.Transport 实例，配置连接池参数
// 使用单例模式确保全局只有一个 Transport 实例，所有连接共享连接池
func (t *HTTPTransport) getTransport() *http.Transport {
	t.once.Do(func() {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			// 不指定 LocalAddr，让系统自动分配端口，避免端口占用冲突
			// 系统会自动选择可用端口，避免 "connectex" 错误
		}

		t.transport = &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: dialer.DialContext,
			// 连接池配置：支持大量并发连接
			MaxIdleConns:          1000,             // 最大空闲连接数
			MaxIdleConnsPerHost:   100,              // 每个 host 的最大空闲连接数（降低以避免端口耗尽）
			MaxConnsPerHost:       0,                // 0 表示不限制每个 host 的总连接数
			IdleConnTimeout:       90 * time.Second, // 空闲连接超时（增加以支持长连接复用）
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: t.timeout,
			// 关键配置：启用连接复用
			DisableKeepAlives:  false, // 必须为 false，启用 Keep-Alive
			ForceAttemptHTTP2:  false, // 当前仅支持 HTTP/1.1，未来可扩展 HTTP/2
			DisableCompression: false,
		}
	})
	return t.transport
}

// getOrCreateProxy 获取或创建指定 target 的 ReverseProxy 实例
// 使用缓存避免重复创建，确保连接管理的稳定性
func (t *HTTPTransport) getOrCreateProxy(targetURL *url.URL) *httputil.ReverseProxy {
	// 使用 target 的完整 URL（scheme + host + path）作为 key
	targetKey := targetURL.String()

	// 尝试从缓存获取
	if cached, ok := t.proxyCache.Load(targetKey); ok {
		cp := cached.(*cachedProxy)
		cp.mu.Lock()
		cp.lastUse = time.Now()
		cp.mu.Unlock()
		return cp.proxy
	}

	// 缓存未命中，创建新的 ReverseProxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// 保存原始 Director
	origDirector := proxy.Director

	// 自定义 Director：正确设置请求信息并保留所有 headers
	proxy.Director = func(r *http.Request) {
		// 先调用原始 Director
		origDirector(r)

		// 合并路径
		r.URL.Path = singleJoiningSlash(targetURL.Path, r.URL.Path)
		r.URL.RawPath = "" // 清空 RawPath，让 Path 生效

		// 设置 Host header（重要：某些服务依赖此 header）
		r.Host = targetURL.Host

		// 关键：保留所有原始 headers（包括 Authorization 等）
		// httputil.NewSingleHostReverseProxy 默认会保留大部分 headers，
		// 但我们需要确保特殊 headers 也被正确转发

		// 确保连接复用
		r.Close = false

		// 移除可能干扰的 headers（如果需要）
		// r.Header.Del("Connection") // 不删除，让 Transport 管理
	}

	// 使用共享的 Transport，支持连接复用
	proxy.Transport = t.getTransport()

	// 自定义错误处理
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		if t.enableLog {
			log.Printf("[HTTPTransport] Proxy error for %s: %v", targetURL.String(), e)
		}
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("proxy error: " + e.Error()))
	}

	// 缓存新的 proxy 实例
	cp := &cachedProxy{
		proxy:   proxy,
		target:  targetURL,
		lastUse: time.Now(),
	}
	t.proxyCache.Store(targetKey, cp)

	return proxy
}

// Proxy 执行代理转发
// 核心方法：根据 target 地址转发请求到后端服务
func (t *HTTPTransport) Proxy(ctx context.Context, target string, req *http.Request, w http.ResponseWriter) error {
	startTime := time.Now()

	// 解析 target URL
	targetURL, err := url.Parse(target)
	if err != nil {
		if t.enableLog {
			log.Printf("[HTTPTransport] Invalid target URL: %s, error: %v", target, err)
		}
		return err
	}

	// 获取或创建 ReverseProxy 实例
	proxy := t.getOrCreateProxy(targetURL)

	// 创建响应包装器以记录状态码
	responseWriter := &responseWriterWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // 默认状态码
	}

	// 记录请求信息
	if t.enableLog {
		log.Printf("[HTTPTransport] Proxying %s %s -> %s%s",
			req.Method, req.URL.Path, targetURL.Host, req.URL.Path)
	}

	// 执行代理转发
	// 使用 context 控制超时
	proxy.ServeHTTP(responseWriter, req.WithContext(ctx))

	// 记录响应信息
	duration := time.Since(startTime)
	if t.enableLog {
		log.Printf("[HTTPTransport] Proxied %s %s -> %s%s [%d] in %v",
			req.Method, req.URL.Path, targetURL.Host, req.URL.Path,
			responseWriter.statusCode, duration)
	}

	return nil
}

// responseWriterWrapper 包装 http.ResponseWriter 以记录状态码
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Close 关闭 Transport 并清理所有空闲连接和缓存
func (t *HTTPTransport) Close() error {
	// 关闭 Transport 的所有空闲连接
	if t.transport != nil {
		t.transport.CloseIdleConnections()
	}

	// 清理缓存（可选，通常不需要，因为程序退出时自动清理）
	t.proxyCache.Range(func(key, value interface{}) bool {
		t.proxyCache.Delete(key)
		return true
	})

	return nil
}

// singleJoiningSlash 合并两个路径，正确处理斜杠
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
