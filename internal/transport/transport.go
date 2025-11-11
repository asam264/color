package transport

import (
	"context"
	"net/http"
)

// Transport 传输层接口（支持 HTTP/gRPC 等）
type Transport interface {
	// Proxy 执行代理转发
	Proxy(ctx context.Context, target string, req *http.Request, w http.ResponseWriter) error
}
