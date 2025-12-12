package transport

import (
	"context"
	"net/http"

	"google.golang.org/grpc"
)

// HTTPTransporter HTTP 传输层接口
type HTTPTransporter interface {
	Proxy(ctx context.Context, target string, req *http.Request, w http.ResponseWriter) error
	Close() error
}

// GRPCTransporter gRPC 传输层接口
type GRPCTransporter interface {
	Proxy(ctx context.Context, target string, method string, req interface{}, reply interface{}, opts ...grpc.CallOption) error
	Close() error
}
