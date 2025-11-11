package backend

import (
	"context"
	"time"
)

// Route 路由信息
type Route struct {
	Color     string
	Address   string
	Owner     string
	Token     string
	ExpiresAt time.Time
}

// Backend 存储后端接口
type Backend interface {
	// Register 注册路由
	Register(ctx context.Context, route *Route, ttl time.Duration) error

	// Get 获取路由
	Get(ctx context.Context, color string) (*Route, error)

	// Heartbeat 心跳续期
	Heartbeat(ctx context.Context, color, address, token string, ttl time.Duration) error

	// List 列出所有路由
	List(ctx context.Context) ([]*Route, error)

	// Delete 删除路由
	Delete(ctx context.Context, color string) error

	// DeleteExpired 清理过期路由
	DeleteExpired(ctx context.Context) error

	// Close 关闭连接
	Close() error
}
