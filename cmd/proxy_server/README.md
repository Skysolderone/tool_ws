# 币安合约 API 代理服务器

## 用途

在**有权访问币安 API 的服务器**上部署此代理服务器，本地客户端通过代理访问币安。

## 使用场景

- ✅ 本地网络无法直接访问币安 API
- ✅ 币安限制特定 IP 访问
- ✅ 需要统一出口 IP
- ✅ 多个客户端共享一个 API Key

## 架构

```
本地客户端（ProxyClient）
    ↓ HTTP
代理服务器（部署在服务器）
    ↓ HTTPS + API Key
币安 Futures API
```

## 服务器部署

### 1. 准备环境

**服务器要求**：
- Go 1.18+
- 能访问币安 API（`https://fapi.binance.com`）
- 开放端口 10087（或自定义端口）

### 2. 上传代码

```bash
# 在服务器上
cd /opt
git clone <your-repo-url> tool_ws
cd tool_ws
```

### 3. 配置环境变量

```bash
# 编辑 ~/.bashrc 或 ~/.profile
export BINANCE_API_KEY="your_api_key_here"
export BINANCE_SECRET_KEY="your_secret_key_here"

# 可选：使用测试网
export BINANCE_TESTNET=true

# 应用配置
source ~/.bashrc
```

或使用 `.env` 文件：

```bash
# 在项目根目录创建 .env
cat > .env << EOF
BINANCE_API_KEY=your_api_key_here
BINANCE_SECRET_KEY=your_secret_key_here
BINANCE_TESTNET=true
EOF

# 加载环境变量
export $(cat .env | xargs)
```

### 4. 编译

```bash
go build -o proxy_server cmd/proxy_server/main.go
```

### 5. 运行

**前台运行**：
```bash
./proxy_server
# 输出:
# Proxy server running on :10087
# Forwarding requests to Binance Futures API
```

**后台运行（使用 nohup）**：
```bash
nohup ./proxy_server > proxy.log 2>&1 &
echo $! > proxy.pid  # 保存进程 ID
```

**后台运行（使用 systemd）**：

创建服务文件 `/etc/systemd/system/binance-proxy.service`：

```ini
[Unit]
Description=Binance Futures API Proxy
After=network.target

[Service]
Type=simple
User=your_username
WorkingDirectory=/opt/tool_ws
Environment="BINANCE_API_KEY=your_api_key"
Environment="BINANCE_SECRET_KEY=your_secret_key"
ExecStart=/opt/tool_ws/proxy_server
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

启动服务：
```bash
sudo systemctl daemon-reload
sudo systemctl enable binance-proxy
sudo systemctl start binance-proxy
sudo systemctl status binance-proxy
```

查看日志：
```bash
sudo journalctl -u binance-proxy -f
```

### 6. 验证部署

```bash
# 健康检查
curl http://localhost:10087/health

# 输出:
# {"status":"ok","service":"binance-futures-proxy"}
```

## 本地客户端配置

### 1. 安装依赖

```bash
go get github.com/adshao/go-binance/v2
```

### 2. 连接到代理服务器

```go
package main

import (
    "context"
    "fmt"
    "tools/api"
    "github.com/adshao/go-binance/v2/futures"
)

func main() {
    // 连接到服务器上的代理（替换为你的服务器 IP）
    client := api.NewProxyClient("http://your-server-ip:10087")

    ctx := context.Background()

    // 查询持仓
    positions, err := client.GetPositions(ctx, "BTCUSDT")
    if err != nil {
        panic(err)
    }

    fmt.Printf("持仓数量: %d\n", len(positions))
}
```

### 3. 运行示例

```bash
# 修改示例中的服务器地址
sed -i 's/localhost/your-server-ip/g' examples/proxy_client.go

# 运行
go run examples/proxy_client.go
```

## 安全配置

### 1. 防火墙配置

**仅允许特定 IP 访问**：

```bash
# Ubuntu/Debian
sudo ufw allow from your_local_ip to any port 10087
sudo ufw enable

# CentOS/RHEL
sudo firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="your_local_ip" port port="10087" protocol="tcp" accept'
sudo firewall-cmd --reload
```

### 2. 使用 HTTPS（推荐）

**使用 Nginx 反向代理 + SSL**：

```nginx
# /etc/nginx/sites-available/binance-proxy
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate /etc/ssl/certs/your-cert.pem;
    ssl_certificate_key /etc/ssl/private/your-key.pem;

    location / {
        proxy_pass http://127.0.0.1:10087;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

启用配置：
```bash
sudo ln -s /etc/nginx/sites-available/binance-proxy /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

本地客户端使用 HTTPS：
```go
client := api.NewProxyClient("https://your-domain.com")
```

### 3. 基本认证（可选）

修改 `cmd/proxy_server/main.go`，添加中间件：

```go
func authMiddleware() app.HandlerFunc {
    return func(c context.Context, ctx *app.RequestContext) {
        token := ctx.GetHeader("Authorization")
        if string(token) != "Bearer your-secret-token" {
            ctx.AbortWithStatusJSON(401, map[string]string{
                "error": "unauthorized",
            })
            return
        }
        ctx.Next(c)
    }
}

func registerRoutes(h *server.Hertz) {
    // 添加认证中间件
    apiGroup := h.Group("/api", authMiddleware())
    {
        // ... 路由
    }
}
```

客户端添加认证：
```go
client := api.NewProxyClient("http://your-server-ip:10087")

// 自定义请求（添加 Authorization header）
// 需要修改 proxy.go 支持自定义 header
```

## 监控和日志

### 1. 查看实时日志

```bash
# systemd 服务
sudo journalctl -u binance-proxy -f

# nohup 方式
tail -f proxy.log
```

### 2. 日志分析

```bash
# 统计请求数
grep "GET\|POST\|DELETE" proxy.log | wc -l

# 查看错误
grep "error\|Error\|ERROR" proxy.log
```

### 3. 性能监控

```bash
# CPU 和内存使用
ps aux | grep proxy_server

# 网络连接数
netstat -an | grep :10087 | wc -l

# 端口监听状态
ss -tlnp | grep 10087
```

## 故障排查

### 问题 1：连接被拒绝

```bash
# 检查服务是否运行
ps aux | grep proxy_server

# 检查端口是否监听
netstat -tlnp | grep 10087

# 重启服务
sudo systemctl restart binance-proxy
```

### 问题 2：币安 API 调用失败

```bash
# 检查服务器能否访问币安
curl -I https://fapi.binance.com/fapi/v1/ping

# 检查 API Key 是否正确
echo $BINANCE_API_KEY
echo $BINANCE_SECRET_KEY
```

### 问题 3：防火墙阻止

```bash
# 检查防火墙规则
sudo ufw status  # Ubuntu
sudo firewall-cmd --list-all  # CentOS

# 临时关闭防火墙测试
sudo ufw disable  # 测试后记得重新启用
```

### 问题 4：环境变量未加载

```bash
# 检查环境变量
env | grep BINANCE

# 确保 systemd 服务文件包含环境变量
sudo systemctl daemon-reload
sudo systemctl restart binance-proxy
```

## 性能优化

### 1. 调整超时时间

修改 `cmd/proxy_server/main.go`：

```go
h := server.Default(
    server.WithHostPorts(":10087"),
    server.WithReadTimeout(60 * time.Second),
    server.WithWriteTimeout(60 * time.Second),
)
```

### 2. 限制并发连接

```go
h := server.Default(
    server.WithHostPorts(":10087"),
    server.WithMaxRequestBodySize(10*1024*1024),
    server.WithIdleTimeout(30 * time.Second),
)
```

### 3. 启用 WebSocket 价格缓存

代理服务器已自动集成 WebSocket 价格缓存，无需额外配置。

## 升级和维护

### 更新代码

```bash
cd /opt/tool_ws
git pull
go build -o proxy_server cmd/proxy_server/main.go
sudo systemctl restart binance-proxy
```

### 备份配置

```bash
# 备份环境变量
cp ~/.bashrc ~/.bashrc.backup

# 备份服务文件
sudo cp /etc/systemd/system/binance-proxy.service /etc/systemd/system/binance-proxy.service.backup
```

## 完整部署脚本

```bash
#!/bin/bash
# deploy-proxy.sh

set -e

echo "=== 部署币安合约 API 代理服务器 ==="

# 1. 检查环境
if [ -z "$BINANCE_API_KEY" ]; then
    echo "错误: BINANCE_API_KEY 未设置"
    exit 1
fi

if [ -z "$BINANCE_SECRET_KEY" ]; then
    echo "错误: BINANCE_SECRET_KEY 未设置"
    exit 1
fi

# 2. 进入项目目录
cd /opt/tool_ws

# 3. 拉取最新代码
git pull

# 4. 编译
echo "编译代理服务器..."
go build -o proxy_server cmd/proxy_server/main.go

# 5. 停止旧服务
echo "停止旧服务..."
sudo systemctl stop binance-proxy || true

# 6. 启动新服务
echo "启动新服务..."
sudo systemctl start binance-proxy

# 7. 检查状态
sleep 2
sudo systemctl status binance-proxy

# 8. 健康检查
echo "健康检查..."
curl http://localhost:10087/health

echo "=== 部署完成 ==="
```

使用方法：
```bash
chmod +x deploy-proxy.sh
./deploy-proxy.sh
```

## API 端点

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/api/positions?symbol=` | 查询持仓 |
| POST | `/api/order` | 下单 |
| GET | `/api/orders?symbol=` | 查询订单 |
| DELETE | `/api/order?symbol=&orderId=` | 取消订单 |
| POST | `/api/leverage` | 调整杠杆 |

## 注意事项

⚠️ **重要提醒：**

1. **API Key 安全**：不要将 API Key 提交到 Git
2. **防火墙配置**：限制只允许可信 IP 访问
3. **使用 HTTPS**：生产环境建议使用 HTTPS
4. **日志轮转**：定期清理日志文件
5. **监控告警**：配置服务异常告警

## 测试

```bash
# 在服务器上测试
curl http://localhost:10087/health
curl http://localhost:10087/api/positions

# 在本地测试
curl http://your-server-ip:10087/health
go run examples/proxy_client.go
```
