package color

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/asam264/color/internal/backend"
	"github.com/asam264/color/internal/strategy"
	"github.com/asam264/color/internal/transport"
	"github.com/gin-gonic/gin"
)

// Proxy 核心代理对象
type Proxy struct {
	backend   backend.Backend
	transport transport.Transport
	strategy  strategy.Strategy

	config *Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config 配置
type Config struct {
	// 存储配置
	Backend backend.Backend

	// 传输配置
	Transport transport.Transport

	// 路由策略
	Strategy strategy.Strategy

	// TTL 配置
	TTL           time.Duration
	HeartbeatRate time.Duration
	CleanupRate   time.Duration

	// 自注册配置（可选）
	AutoRegister bool
	LocalColor   string
	LocalAddress string
	LocalToken   string
	LocalOwner   string

	// 日志
	Logger Logger
}

// Logger 日志接口
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

type defaultLogger struct{}

func (l *defaultLogger) Info(msg string, args ...interface{}) {
	log.Printf("[ColorProxy] "+msg, args...)
}

func (l *defaultLogger) Error(msg string, args ...interface{}) {
	log.Printf("[ColorProxy ERROR] "+msg, args...)
}

// Option 配置选项
type Option func(*Config)

// WithRedis 使用 Redis 后端
func WithRedis(addr, password string, db int) Option {
	return func(c *Config) {
		backend, err := backend.NewRedisBackend(&backend.RedisConfig{
			Addr:     addr,
			Password: password,
			DB:       db,
		})
		if err != nil {
			panic(err) // 初始化失败直接panic，外部可以recover
		}
		c.Backend = backend
	}
}

// WithBackend 自定义后端
func WithBackend(b backend.Backend) Option {
	return func(c *Config) {
		c.Backend = b
	}
}

// WithHTTPTransport 使用 HTTP 传输
func WithHTTPTransport(timeout time.Duration) Option {
	return func(c *Config) {
		c.Transport = transport.NewHTTPTransport(timeout)
	}
}

// WithTransport 自定义传输层
func WithTransport(t transport.Transport) Option {
	return func(c *Config) {
		c.Transport = t
	}
}

// WithSimpleStrategy 使用简单路由策略
func WithSimpleStrategy() Option {
	return func(c *Config) {
		// strategy 需要在 backend 设置后初始化
	}
}

// WithStrategy 自定义路由策略
func WithStrategy(s strategy.Strategy) Option {
	return func(c *Config) {
		c.Strategy = s
	}
}

// WithTTL 设置路由过期时间
func WithTTL(ttl time.Duration) Option {
	return func(c *Config) {
		c.TTL = ttl
	}
}

// WithAutoRegister 启用自动注册
func WithAutoRegister(color, address, token, owner string) Option {
	return func(c *Config) {
		// 只有在 color 和 address 都不为空时才启用
		if color != "" && address != "" {
			// 如果 address 只是端口号，自动获取本机 IP
			if !isFullURL(address) {
				localIP := getLocalIPv4()
				if localIP != "" {
					address = fmt.Sprintf("http://%s:%s", localIP, address)
				} else {
					c.Logger.Error("failed to get local IP, auto register disabled")
					return
				}
			}

			c.AutoRegister = true
			c.LocalColor = color
			c.LocalAddress = address
			c.LocalToken = token
			c.LocalOwner = owner
		}
	}
}

// isFullURL 判断是否是完整 URL
func isFullURL(addr string) bool {
	return len(addr) > 7 && (addr[:7] == "http://" || addr[:8] == "https://")
}

// getLocalIPv4 获取本机非回环 IPv4 地址
func getLocalIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return ""
}

// WithLogger 自定义日志
func WithLogger(logger Logger) Option {
	return func(c *Config) {
		c.Logger = logger
	}
}

// New 创建代理实例
func New(opts ...Option) (*Proxy, error) {
	// 默认配置
	cfg := &Config{
		TTL:           2 * time.Minute,
		HeartbeatRate: 30 * time.Second,
		CleanupRate:   1 * time.Minute,
		Logger:        &defaultLogger{},
	}

	// 应用选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 检查必需配置
	if cfg.Backend == nil {
		return nil, ErrBackendRequired
	}
	if cfg.Transport == nil {
		cfg.Transport = transport.NewHTTPTransport(30 * time.Second)
	}
	if cfg.Strategy == nil {
		cfg.Strategy = strategy.NewSimpleStrategy(cfg.Backend)
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Proxy{
		backend:   cfg.Backend,
		transport: cfg.Transport,
		strategy:  cfg.Strategy,
		config:    cfg,
		ctx:       ctx,
		cancel:    cancel,
	}

	// 启动后台任务
	p.startBackgroundTasks()

	// 自动注册
	if cfg.AutoRegister {
		if err := p.registerSelf(); err != nil {
			cfg.Logger.Error("auto register failed: %v", err)
		}
	}

	cfg.Logger.Info("proxy initialized")
	return p, nil
}

// AttachGin 集成到 Gin 引擎
func (p *Proxy) AttachGin(engine *gin.Engine) {
	// 注册管理端点
	api := engine.Group("/colorproxy")
	{
		api.POST("/register", p.ginHandleRegister)
		api.POST("/heartbeat", p.ginHandleHeartbeat)
		api.GET("/routes", p.ginHandleListRoutes)
		api.DELETE("/routes/:color", p.ginHandleDeleteRoute)
	}

	// 全局代理中间件
	engine.Use(p.ginProxyMiddleware())

	p.config.Logger.Info("attached to Gin engine")
}

// startBackgroundTasks 启动后台任务
func (p *Proxy) startBackgroundTasks() {
	// 清理过期路由
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.config.CleanupRate)
		defer ticker.Stop()

		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ticker.C:
				if err := p.backend.DeleteExpired(p.ctx); err != nil {
					p.config.Logger.Error("cleanup expired failed: %v", err)
				}
			}
		}
	}()

	// 自动心跳
	if p.config.AutoRegister {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			ticker := time.NewTicker(p.config.HeartbeatRate)
			defer ticker.Stop()

			for {
				select {
				case <-p.ctx.Done():
					return
				case <-ticker.C:
					if err := p.heartbeatSelf(); err != nil {
						p.config.Logger.Error("heartbeat failed: %v", err)
					}
				}
			}
		}()
	}
}

// registerSelf 自注册
func (p *Proxy) registerSelf() error {
	route := &backend.Route{
		Color:   p.config.LocalColor,
		Address: p.config.LocalAddress,
		Owner:   p.config.LocalOwner,
		Token:   p.config.LocalToken,
	}

	if err := p.backend.Register(p.ctx, route, p.config.TTL); err != nil {
		return err
	}

	p.config.Logger.Info("self registered: color=%s, addr=%s", route.Color, route.Address)
	return nil
}

// heartbeatSelf 自心跳
func (p *Proxy) heartbeatSelf() error {
	return p.backend.Heartbeat(
		p.ctx,
		p.config.LocalColor,
		p.config.LocalAddress,
		p.config.LocalToken,
		p.config.TTL,
	)
}

// Gin handlers
func (p *Proxy) ginHandleRegister(c *gin.Context) {
	var req struct {
		Color   string `json:"color" binding:"required"`
		Address string `json:"address" binding:"required"`
		Owner   string `json:"owner"`
		Token   string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	route := &backend.Route{
		Color:   req.Color,
		Address: req.Address,
		Owner:   req.Owner,
		Token:   req.Token,
	}

	if err := p.backend.Register(c.Request.Context(), route, p.config.TTL); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "registered", "color": req.Color})
}

func (p *Proxy) ginHandleHeartbeat(c *gin.Context) {
	var req struct {
		Color   string `json:"color" binding:"required"`
		Address string `json:"address" binding:"required"`
		Token   string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if err := p.backend.Heartbeat(c.Request.Context(), req.Color, req.Address, req.Token, p.config.TTL); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "heartbeat ok"})
}

func (p *Proxy) ginHandleListRoutes(c *gin.Context) {
	routes, err := p.backend.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"routes": routes, "count": len(routes)})
}

func (p *Proxy) ginHandleDeleteRoute(c *gin.Context) {
	color := c.Param("color")
	if err := p.backend.Delete(c.Request.Context(), color); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "deleted", "color": color})
}

func (p *Proxy) ginProxyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		color := c.GetHeader("color")
		if color == "" {
			c.Next()
			return
		}

		// 使用策略选择目标
		target, err := p.strategy.Select(c.Request.Context(), color)
		if err != nil {
			// 如果找不到匹配的 color 服务，继续正常处理请求，不进行转发
			p.config.Logger.Info("route not found for color=%s, continuing normal request handling", color)
			c.Next()
			return
		}

		// 使用传输层转发
		if err := p.transport.Proxy(c.Request.Context(), target, c.Request, c.Writer); err != nil {
			c.JSON(502, gin.H{"error": "proxy failed", "detail": err.Error()})
			c.Abort()
			return
		}

		c.Abort()
	}
}

// Shutdown 优雅退出：停止后台任务、删除自己的注册、关闭所有资源
func (p *Proxy) Shutdown(ctx context.Context) error {
	p.config.Logger.Info("shutting down proxy...")

	// 停止后台任务
	p.cancel()

	// 如果启用了自动注册，删除自己的注册
	if p.config.AutoRegister && p.config.LocalColor != "" {
		if err := p.backend.Delete(ctx, p.config.LocalColor); err != nil {
			p.config.Logger.Error("failed to delete self registration: %v", err)
		} else {
			p.config.Logger.Info("deleted self registration: color=%s", p.config.LocalColor)
		}
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		p.config.Logger.Error("shutdown timeout")
		return ctx.Err()
	case <-done:
		// 后台任务已完成
	}

	// 关闭 transport
	if closer, ok := p.transport.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			p.config.Logger.Error("close transport failed: %v", err)
		}
	}

	// 关闭 backend
	if p.backend != nil {
		if err := p.backend.Close(); err != nil {
			p.config.Logger.Error("close backend failed: %v", err)
			return err
		}
	}

	p.config.Logger.Info("proxy shutdown completed")
	return nil
}

// Close 关闭资源（兼容旧接口，内部调用 Shutdown）
func (p *Proxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return p.Shutdown(ctx)
}

// 错误定义
var (
	ErrBackendRequired = &ProxyError{Code: "BACKEND_REQUIRED", Message: "backend is required"}
)

type ProxyError struct {
	Code    string
	Message string
}

func (e *ProxyError) Error() string {
	return e.Message
}
