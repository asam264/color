package transport

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// GRPCTransport gRPC 传输层实现
type GRPCTransport struct {
	timeout   time.Duration
	enableLog bool

	// gRPC 客户端连接池：缓存到不同 target 的连接
	connPool sync.Map // map[string]*grpcConn

	// 连接池清理
	mu            sync.RWMutex
	closeOnce     sync.Once
	cleanupTicker *time.Ticker
	done          chan struct{}
}

type grpcConn struct {
	conn    *grpc.ClientConn
	target  string
	mu      sync.RWMutex
	lastUse time.Time
}

// NewGRPCTransport 创建 gRPC 传输层
func NewGRPCTransport(timeout time.Duration) *GRPCTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	t := &GRPCTransport{
		timeout:   timeout,
		enableLog: true,
		done:      make(chan struct{}),
	}

	// 启动连接清理协程（清理超过 5 分钟未使用的连接）
	t.cleanupTicker = time.NewTicker(1 * time.Minute)
	go t.cleanupIdleConnections()

	return t
}

// getOrCreateConn 获取或创建到 target 的 gRPC 连接
func (t *GRPCTransport) getOrCreateConn(target string) (*grpc.ClientConn, error) {
	// 解析 target（支持 "host:port" 或 "grpc://host:port" 格式）
	addr := target
	if targetURL, err := url.Parse(target); err == nil && targetURL.Host != "" {
		addr = targetURL.Host
	}

	if addr == "" {
		return nil, fmt.Errorf("empty target address")
	}

	// 从缓存获取
	if cached, ok := t.connPool.Load(addr); ok {
		gc := cached.(*grpcConn)
		gc.mu.Lock()
		gc.lastUse = time.Now()
		gc.mu.Unlock()
		return gc.conn, nil
	}

	// 创建新连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 连接选项
	opts := []grpc.DialOption{
		// 本地开发使用，生产环境应使用 TLS
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// 消息大小限制（根据实际需求调整）
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(100*1024*1024), // 100MB
			grpc.MaxCallSendMsgSize(100*1024*1024), // 100MB
		),
		// Keepalive 配置：保持长连接
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // 每 10 秒发送 keepalive ping
			Timeout:             3 * time.Second,  // ping 超时时间
			PermitWithoutStream: true,             // 允许无活跃流时发送 ping
		}),
	}

	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	// 缓存连接
	gc := &grpcConn{
		conn:    conn,
		target:  addr,
		lastUse: time.Now(),
	}
	t.connPool.Store(addr, gc)

	if t.enableLog {
		log.Printf("[GRPCTransport] Created new connection to %s", addr)
	}

	return conn, nil
}

// ProxyUnary 执行 gRPC Unary RPC 代理转发
func (t *GRPCTransport) Proxy(
	ctx context.Context,
	target string,
	method string,
	req interface{},
	reply interface{},
	opts ...grpc.CallOption,
) error {
	conn, err := t.getOrCreateConn(target)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}

	// 创建带超时的 context
	proxyCtx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	// 从原 context 提取 metadata 并传递到新 context
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		proxyCtx = metadata.NewOutgoingContext(proxyCtx, md)
		if t.enableLog {
			if colorValues := md.Get("color"); len(colorValues) > 0 {
				log.Printf("[GRPCTransport] Forwarding request with color=%s to %s%s",
					colorValues[0], target, method)
			}
		}
	} else if md, ok := metadata.FromIncomingContext(ctx); ok {
		proxyCtx = metadata.NewOutgoingContext(proxyCtx, md)
	}

	// 使用 Invoke 进行通用 RPC 调用
	err = conn.Invoke(proxyCtx, method, req, reply, opts...)
	if err != nil {
		if t.enableLog {
			log.Printf("[GRPCTransport] RPC call failed: method=%s, target=%s, error=%v",
				method, target, err)
		}
		return fmt.Errorf("rpc call failed: %w", err)
	}

	if t.enableLog {
		log.Printf("[GRPCTransport] RPC call succeeded: method=%s, target=%s", method, target)
	}

	return nil
}

// cleanupIdleConnections 定期清理空闲连接
func (t *GRPCTransport) cleanupIdleConnections() {
	for {
		select {
		case <-t.cleanupTicker.C:
			now := time.Now()
			t.connPool.Range(func(key, value interface{}) bool {
				gc := value.(*grpcConn)
				gc.mu.RLock()
				idle := now.Sub(gc.lastUse)
				gc.mu.RUnlock()

				// 超过 5 分钟未使用，关闭连接
				if idle > 5*time.Minute {
					gc.mu.Lock()
					gc.conn.Close()
					gc.mu.Unlock()
					t.connPool.Delete(key)

					if t.enableLog {
						log.Printf("[GRPCTransport] Closed idle connection to %s", gc.target)
					}
				}
				return true
			})
		case <-t.done:
			return
		}
	}
}

// Close 关闭所有连接
func (t *GRPCTransport) Close() error {
	t.closeOnce.Do(func() {
		// 停止清理协程
		close(t.done)
		if t.cleanupTicker != nil {
			t.cleanupTicker.Stop()
		}

		// 关闭所有连接
		t.connPool.Range(func(key, value interface{}) bool {
			gc := value.(*grpcConn)
			gc.mu.Lock()
			if gc.conn != nil {
				gc.conn.Close()
			}
			gc.mu.Unlock()
			return true
		})

		// 清空连接池
		t.connPool.Range(func(key, value interface{}) bool {
			t.connPool.Delete(key)
			return true
		})
	})

	return nil
}
