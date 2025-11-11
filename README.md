# color
Color is a lightweight HTTP/gRPC forwarder designed for local debugging.


## 🎯 设计理念

**分层架构，简单使用**

```
┌─────────────────────────────────────┐
│         User Application            │  外部使用：简单的 Option 配置
├─────────────────────────────────────┤
│      Proxy Core (color.go)          │  核心层：编排各组件
├──────────┬──────────┬───────────────┤
│ Backend  │Transport │   Strategy    │  扩展层：可插拔实现
│ (存储)   │ (传输)   │   (路由策略)  │
├──────────┼──────────┼───────────────┤
│ Redis    │  HTTP    │    Simple     │  实现层：当前实现
│ Etcd     │  gRPC    │  RoundRobin   │  实现层：未来扩展
└──────────┴──────────┴───────────────┘
```

## ✨ 特性

- **分层设计**：Backend / Transport / Strategy 三层抽象
- **易于扩展**：新增后端/传输/策略只需实现接口
- **简单使用**：Option 模式配置，一行集成
- **自动化**：自动注册、心跳、清理过期路由

## 🚀 快速开始

```go
// 1. 创建代理（使用 Redis + HTTP）
proxy, _ := color.New(
    color.WithRedis("localhost:6379", "", 0),
    color.WithHTTPTransport(30*time.Second),
    color.WithAutoRegister("blue", "http://localhost:8080", "token", "owner"),
)
defer proxy.Close()

// 2. 集成到 Gin
r := gin.Default()
proxy.AttachGin(r)

// 3. 定义业务路由
r.GET("/api/users", handleUsers)

r.Run(":8080")
```

## 📦 扩展示例

### 未来扩展 Etcd 后端

```go
// 1. 实现 backend.Backend 接口
type EtcdBackend struct { ... }

// 2. 添加 Option
func WithEtcd(endpoints []string) Option {
    return func(c *Config) {
        c.Backend = NewEtcdBackend(endpoints)
    }
}

// 3. 使用
proxy, _ := color.New(
    color.WithEtcd([]string{"localhost:2379"}),
    ...
)
```

### 未来扩展 gRPC 传输

```go
// 1. 实现 transport.Transport 接口
type GRPCTransport struct { ... }

// 2. 添加 Option
func WithGRPCTransport() Option { ... }

// 3. 使用
proxy, _ := color.New(
    color.WithGRPCTransport(),
    ...
)
```

### 未来扩展负载均衡策略

```go
// 1. 实现 strategy.Strategy 接口
type RoundRobinStrategy struct { ... }

// 2. 使用
proxy, _ := color.New(
    color.WithStrategy(NewRoundRobinStrategy()),
    ...
)
```

## 📁 目录结构

```
colorproxy/
├── color.go                    # 核心入口
├── internal/
│   ├── backend/               # 存储后端
│   │   ├── backend.go         # 接口定义
│   │   ├── redis.go           # Redis 实现
│   │   └── etcd.go            # Etcd 实现（待开发）
│   ├── transport/             # 传输层
│   │   ├── transport.go       # 接口定义
│   │   ├── http.go            # HTTP 实现
│   │   └── grpc.go            # gRPC 实现（待开发）
│   └── strategy/              # 路由策略
│       ├── strategy.go        # 接口定义
│       ├── simple.go          # 简单策略
│       └── roundrobin.go      # 轮询策略（待开发）
└── example/
    └── main.go
```

## 🔌 API 端点

自动注册的管理端点：

- `POST /colorproxy/register` - 注册路由
- `POST /colorproxy/heartbeat` - 心跳续期
- `GET /colorproxy/routes` - 列出所有路由
- `DELETE /colorproxy/routes/:color` - 删除路由

## 🎯 使用场景

1. **微服务灰度发布**：通过 color header 路由到不同版本
2. **多租户隔离**：不同租户使用不同 color
3. **AB 测试**：不同 color 对应不同实验组
4. **开发环境隔离**：dev/test/prod 环境隔离

简洁、优雅、可扩展！