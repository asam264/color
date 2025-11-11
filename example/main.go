package main

import (
	"github.com/asam264/color"
	"log"
	"time"
	
	"github.com/gin-gonic/gin"
)

func main() {
	// 创建代理实例 - 使用 Option 模式配置
	proxy, err := color.New(
		// 选择存储后端：Redis
		color.WithRedis("localhost:6379", "", 0),

		// 选择传输层：HTTP
		color.WithHTTPTransport(30*time.Second),

		// 选择路由策略：简单策略（默认）
		color.WithSimpleStrategy(),

		// 配置 TTL
		color.WithTTL(2*time.Minute),

		// 启用自动注册
		color.WithAutoRegister(
			"blue",                  // color
			"http://localhost:8080", // address
			"my-secret-token",       // token
			"service-blue",          // owner
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer proxy.Close()

	// 创建 Gin 引擎
	r := gin.Default()

	// 一行代码集成所有功能
	proxy.AttachGin(r)

	// 业务路由
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
			"service": "blue",
		})
	})

	r.GET("/api/users", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"users": []string{"alice", "bob", "charlie"},
		})
	})

	log.Println("Server started at :8080")
	r.Run(":8080")
}
