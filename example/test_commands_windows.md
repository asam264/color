# Windows 测试命令

## 运行测试服务器

```powershell
# 进入测试目录
cd example\test

# 运行测试服务器（需要先启动 Redis）
go run main.go
```

## 问题说明
Windows 的 curl 对单引号的处理与 Linux 不同，需要使用双引号。

## 正确的测试命令

### 1. 测试 red 服务
```powershell
curl -H "color: red" http://localhost:8080/ping
curl -H "color: red" http://localhost:8080/api/test
```

### 2. 测试 blue 服务
```powershell
curl -H "color: blue" http://localhost:8080/ping
curl -H "color: blue" http://localhost:8080/api/test
```

### 3. 测试 green 服务
```powershell
curl -H "color: green" http://localhost:8080/ping
curl -H "color: green" http://localhost:8080/api/test
```

### 4. 测试 Authorization header 转发
```powershell
curl -H "color: red" -H "Authorization: Bearer test-token" http://localhost:8080/api/test
```

### 5. 查看已注册的路由
```powershell
curl http://localhost:8080/colorproxy/routes
```

## 使用 PowerShell 的 Invoke-WebRequest（替代方案）

如果 curl 有问题，可以使用 PowerShell：

```powershell
# 测试 red 服务
$headers = @{ "color" = "red" }
Invoke-WebRequest -Uri "http://localhost:8080/ping" -Headers $headers

# 测试带 Authorization
$headers = @{ 
    "color" = "red"
    "Authorization" = "Bearer test-token"
}
Invoke-WebRequest -Uri "http://localhost:8080/api/test" -Headers $headers
```

## 调试步骤

1. **检查服务是否注册成功**：
   ```powershell
   curl http://localhost:8080/colorproxy/routes
   ```
   应该看到 red、blue、green 三个服务

2. **检查后端服务是否运行**：
   ```powershell
   curl http://localhost:7888/ping
   curl http://localhost:7889/ping
   curl http://localhost:7890/ping
   ```

3. **查看代理日志**：
   运行 `test_proxy.go` 时，应该能看到：
   - `[ColorProxy] received request with color=red, path=/ping`
   - `[ColorProxy] routing color=red to target=http://localhost:7888`
   - `[HTTPTransport] Proxying GET /ping -> localhost:7888/ping`

