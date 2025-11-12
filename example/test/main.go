package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/asam264/color"
	"github.com/gin-gonic/gin"
)

// 测试用的后端服务模拟器
func startMockBackend(port int, colorName string) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
			"color":   colorName,
			"port":    port,
		})
	})

	r.GET("/api/test", func(c *gin.Context) {
		// 返回请求的 headers，用于验证 header 转发
		headers := make(map[string]string)
		for k, v := range c.Request.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		c.JSON(200, gin.H{
			"color":   colorName,
			"port":    port,
			"path":    c.Request.URL.Path,
			"headers": headers,
		})
	})

	r.POST("/api/echo", func(c *gin.Context) {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err == nil {
			c.JSON(200, gin.H{
				"color": colorName,
				"port":  port,
				"echo":  body,
			})
		} else {
			c.JSON(400, gin.H{"error": err.Error()})
		}
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[Backend %s] Starting on %s", colorName, addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("[Backend %s] Failed to start: %v", colorName, err)
	}
}

// 注册服务到 Redis
func registerService(proxyAddr, color, targetAddr, token string) error {
	url := fmt.Sprintf("http://localhost%s/colorproxy/register", proxyAddr)

	payload := map[string]string{
		"color":   color,
		"address": targetAddr,
		"token":   token,
		"owner":   "test",
	}

	jsonData, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}
	return nil
}

func main() {
	log.Println("=== Color Proxy Test ===")

	// 启动模拟后端服务
	go startMockBackend(7888, "red")
	go startMockBackend(7889, "blue")
	go startMockBackend(7890, "green")

	// 等待后端服务启动
	time.Sleep(2 * time.Second)

	// 创建 color 代理实例
	proxy, err := color.New(
		// 使用 Redis 后端（需要本地运行 Redis）
		color.WithRedis("192.168.0.188:6379", "Zs1XVVUAn77", 8),

		// HTTP 传输层，30秒超时
		color.WithHTTPTransport(30*time.Second),

		// 简单路由策略
		color.WithSimpleStrategy(),

		// TTL 设置为 5 分钟
		color.WithTTL(5*time.Minute),
	)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}
	defer proxy.Close()

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 集成代理
	proxy.AttachGin(r)

	// 注册测试路由
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Color Proxy Test Server",
			"usage":   "Add 'color' header to your request (e.g., 'red', 'blue', 'green')",
		})
	})

	// 启动代理服务器
	proxyPort := ":8080"
	log.Printf("[Proxy] Starting on %s", proxyPort)

	// 在后台注册服务
	go func() {
		time.Sleep(1 * time.Second)
		log.Println("[Test] Registering services...")

		// 注册 red 服务
		if err := registerService(proxyPort, "red", "http://localhost:7888", "token-red"); err != nil {
			log.Printf("[Test] Failed to register red: %v", err)
		} else {
			log.Println("[Test] Registered: red -> http://localhost:7888")
		}

		// 注册 blue 服务
		if err := registerService(proxyPort, "blue", "http://localhost:7889", "token-blue"); err != nil {
			log.Printf("[Test] Failed to register blue: %v", err)
		} else {
			log.Println("[Test] Registered: blue -> http://localhost:7889")
		}

		// 注册 green 服务
		if err := registerService(proxyPort, "green", "http://localhost:7890", "token-green"); err != nil {
			log.Printf("[Test] Failed to register green: %v", err)
		} else {
			log.Println("[Test] Registered: green -> http://localhost:7890")
		}

		log.Println("\n=== Test Commands ===")
		log.Println("Test red service:")
		log.Println("  curl -H 'color: red' http://localhost:8080/ping")
		log.Println("  curl -H 'color: red' http://localhost:8080/api/test")
		log.Println("\nTest blue service:")
		log.Println("  curl -H 'color: blue' http://localhost:8080/ping")
		log.Println("  curl -H 'color: blue' http://localhost:8080/api/test")
		log.Println("\nTest green service:")
		log.Println("  curl -H 'color: green' http://localhost:8080/ping")
		log.Println("  curl -H 'color: green' http://localhost:8080/api/test")
		log.Println("\nTest with Authorization header:")
		log.Println("  curl -H 'color: red' -H 'Authorization: Bearer test-token' http://localhost:8080/api/test")
		log.Println("\n=== Server Running ===")
	}()

	// 启动服务器
	if err := r.Run(proxyPort); err != nil {
		log.Fatalf("Failed to start proxy server: %v", err)
	}
}
