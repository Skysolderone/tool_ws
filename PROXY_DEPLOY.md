# 代理服务器部署指南

## 架构说明

```
本地电脑（无法访问币安）
    ↓ HTTP
服务器（部署代理，IP 可访问币安）
    ↓ HTTPS + API Key 签名
币安 Futures API
```

**核心原理**：
- 代理服务器部署在**能访问币安的服务器**上
- 本地客户端通过代理访问币安，无需直接连接
- API Key 和签名都在服务器端完成

---

## 🚀 快速部署（服务器端）

### 方式 1：自动部署脚本

```bash
# 1. 上传代码到服务器
scp -r /Users/rubioc/tool_ws user@your-server:/opt/

# 2. SSH 登录服务器
ssh user@your-server

# 3. 配置环境变量
export BINANCE_API_KEY="your_api_key"
export BINANCE_SECRET_KEY="your_secret_key"
export BINANCE_TESTNET=true  # 可选，使用测试网

# 4. 运行自动部署脚本
cd /opt/tool_ws
chmod +x scripts/deploy-proxy.sh
./scripts/deploy-proxy.sh

# 完成！服务已启动在 :8888 端口
```

### 方式 2：手动部署

```bash
# 1. 编译
cd /opt/tool_ws
go build -o proxy_server cmd/proxy_server/main.go

# 2. 运行（前台测试）
export BINANCE_API_KEY="your_key"
export BINANCE_SECRET_KEY="your_secret"
./proxy_server

# 3. 或使用 systemd 后台运行
sudo cp scripts/binance-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable binance-proxy
sudo systemctl start binance-proxy
```

### 验证部署

```bash
# 健康检查
curl http://localhost:8888/health
# 输出: {"status":"ok","service":"binance-futures-proxy"}

# 查询持仓测试
curl http://localhost:8888/api/positions

# 查看日志
sudo journalctl -u binance-proxy -f
```

---

## 💻 本地客户端使用

### 1. 修改服务器地址

编辑 `examples/proxy_client_remote.go`：

```go
// 替换为你的服务器 IP
proxyURL := "http://123.456.789.012:8888"
```

### 2. 运行客户端

```bash
go run examples/proxy_client_remote.go
```

### 3. 在代码中使用

```go
package main

import (
    "context"
    "tools/api"
    "github.com/adshao/go-binance/v2/futures"
)

func main() {
    // 连接到远程代理服务器
    client := api.NewProxyClient("http://your-server-ip:8888")

    ctx := context.Background()

    // 下单：5 USDT @ 10x 杠杆
    req := api.PlaceOrderReq{
        Symbol:        "BTCUSDT",
        Side:          futures.SideTypeBuy,
        OrderType:     futures.OrderTypeMarket,
        QuoteQuantity: "5",   // ✅ 必填
        Leverage:      10,    // ✅ 必填
    }

    resp, err := client.PlaceOrder(ctx, req)
    // 所有请求都通过代理转发到币安
}
```

---

## 🔒 安全配置

### 1. 防火墙限制（推荐）

**只允许你的本地 IP 访问**：

```bash
# 服务器上执行
sudo ufw allow from YOUR_LOCAL_IP to any port 8888
sudo ufw enable
```

### 2. 使用 HTTPS（生产环境推荐）

**通过 Nginx 反向代理**：

```nginx
# /etc/nginx/sites-available/binance-proxy
server {
    listen 443 ssl;
    server_name proxy.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8888;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

本地客户端改用 HTTPS：
```go
client := api.NewProxyClient("https://proxy.yourdomain.com")
```

### 3. 环境变量安全

```bash
# 不要在命令行直接输入 API Key
# 使用 .env 文件（不要提交到 Git）

cat > /opt/tool_ws/.env << EOF
BINANCE_API_KEY=your_key_here
BINANCE_SECRET_KEY=your_secret_here
EOF

# 加载环境变量
export $(cat /opt/tool_ws/.env | xargs)
```

---

## 📊 管理命令

### systemd 服务管理

```bash
# 启动
sudo systemctl start binance-proxy

# 停止
sudo systemctl stop binance-proxy

# 重启
sudo systemctl restart binance-proxy

# 查看状态
sudo systemctl status binance-proxy

# 查看日志
sudo journalctl -u binance-proxy -f

# 开机自启
sudo systemctl enable binance-proxy
```

### 进程管理（nohup 方式）

```bash
# 启动
nohup ./proxy_server > proxy.log 2>&1 &
echo $! > proxy.pid

# 停止
kill $(cat proxy.pid)

# 查看日志
tail -f proxy.log
```

---

## 🧪 测试

### 服务器端测试

```bash
# 健康检查
curl http://localhost:8888/health

# 查询持仓
curl http://localhost:8888/api/positions

# 下单测试（JSON）
curl -X POST http://localhost:8888/api/order \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "BTCUSDT",
    "side": "BUY",
    "orderType": "MARKET",
    "quoteQuantity": "5",
    "leverage": 10
  }'
```

### 本地测试

```bash
# 替换为你的服务器 IP
SERVER_IP="123.456.789.012"

# 健康检查
curl http://$SERVER_IP:8888/health

# 运行示例
go run examples/proxy_client_remote.go
```

---

## 🛠️ 故障排查

### 问题 1：连接被拒绝

```bash
# 检查服务是否运行
ps aux | grep proxy_server

# 检查端口
netstat -tlnp | grep 8888

# 检查防火墙
sudo ufw status
```

**解决方法**：
- 确保服务已启动
- 检查防火墙是否允许 8888 端口
- 使用 `sudo systemctl restart binance-proxy`

### 问题 2：本地连接超时

```bash
# 测试网络连通性
ping your-server-ip

# 测试端口
telnet your-server-ip 8888
# 或
nc -zv your-server-ip 8888
```

**解决方法**：
- 检查服务器防火墙规则
- 确保云服务商安全组开放了 8888 端口
- 尝试临时关闭防火墙测试：`sudo ufw disable`

### 问题 3：币安 API 调用失败

```bash
# 服务器上测试币安连通性
curl -I https://fapi.binance.com/fapi/v1/ping

# 检查环境变量
echo $BINANCE_API_KEY
```

**解决方法**：
- 确保服务器能访问币安 API
- 检查 API Key 是否正确
- 查看服务日志：`sudo journalctl -u binance-proxy -n 100`

### 问题 4：下单报错 "quoteQuantity is required"

**原因**：`QuoteQuantity` 和 `Leverage` 是必填字段

**解决方法**：
```go
req := api.PlaceOrderReq{
    Symbol:        "BTCUSDT",
    QuoteQuantity: "5",   // ✅ 必填
    Leverage:      10,    // ✅ 必填
    // ... 其他字段
}
```

---

## 📈 性能优化

### 1. WebSocket 价格缓存

代理服务器已自动集成 WebSocket 价格缓存：
- ⚡ 实时价格（< 100ms 延迟）
- 🔄 自动订阅
- 📉 减少 API 调用

### 2. 连接池

默认 HTTP 客户端已启用连接复用，无需额外配置。

### 3. 监控指标

```bash
# CPU 和内存
top -p $(pgrep proxy_server)

# 网络连接数
netstat -an | grep :8888 | wc -l

# 请求统计
sudo journalctl -u binance-proxy | grep "GET\|POST" | wc -l
```

---

## 📝 完整流程示例

### 1. 服务器部署

```bash
# 登录服务器
ssh user@your-server

# 设置环境变量
export BINANCE_API_KEY="your_key"
export BINANCE_SECRET_KEY="your_secret"

# 部署
cd /opt/tool_ws
chmod +x scripts/deploy-proxy.sh
./scripts/deploy-proxy.sh

# 验证
curl http://localhost:8888/health
```

### 2. 本地使用

```bash
# 修改示例代码中的服务器地址
vim examples/proxy_client_remote.go
# 将 "your-server-ip" 改为实际 IP

# 运行
go run examples/proxy_client_remote.go
```

### 3. 生产环境配置

```bash
# 1. 配置 HTTPS（Nginx）
sudo apt install nginx certbot python3-certbot-nginx
sudo certbot --nginx -d proxy.yourdomain.com

# 2. 限制访问 IP
sudo ufw allow from YOUR_IP to any port 443

# 3. 监控告警
# 配置监控工具（Prometheus、Grafana 等）
```

---

## 🎯 API 端点列表

| 方法 | 路径 | 功能 | 示例 |
|------|------|------|------|
| GET | `/health` | 健康检查 | `curl http://server:8888/health` |
| GET | `/api/positions` | 查询持仓 | `curl http://server:8888/api/positions?symbol=BTCUSDT` |
| POST | `/api/order` | 下单 | 见下方示例 |
| GET | `/api/orders` | 查询订单 | `curl http://server:8888/api/orders` |
| DELETE | `/api/order` | 取消订单 | `curl -X DELETE http://server:8888/api/order?symbol=BTCUSDT&orderId=123` |
| POST | `/api/leverage` | 调整杠杆 | `curl -X POST -d '{"symbol":"BTCUSDT","leverage":10}'` |

### 下单 API 示例

```bash
curl -X POST http://your-server:8888/api/order \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "BTCUSDT",
    "side": "BUY",
    "orderType": "MARKET",
    "quoteQuantity": "5",
    "leverage": 10,
    "positionSide": "LONG"
  }'
```

---

## 📚 相关文档

- **代理服务器详细文档**：`cmd/proxy_server/README.md`
- **客户端 API 文档**：`api/README_PROXY.md`
- **价格缓存说明**：`api/README_PRICE_CACHE.md`
- **使用示例**：`examples/proxy_client_remote.go`

---

## ⚠️ 注意事项

1. **API Key 安全**：
   - 不要将 API Key 提交到 Git
   - 使用环境变量或 `.env` 文件
   - 定期轮换 API Key

2. **网络安全**：
   - 配置防火墙限制访问
   - 生产环境使用 HTTPS
   - 考虑使用 VPN 或私有网络

3. **监控告警**：
   - 监控服务运行状态
   - 设置异常告警
   - 定期检查日志

4. **资源限制**：
   - 注意币安 API 限流
   - 避免频繁重启服务
   - 合理设置超时时间

---

## ✅ 部署检查清单

- [ ] 服务器能访问币安 API
- [ ] Go 1.18+ 已安装
- [ ] 环境变量已配置
- [ ] 代码已上传到服务器
- [ ] 代理服务已启动
- [ ] 健康检查通过
- [ ] 防火墙已配置
- [ ] 本地客户端能连接
- [ ] 测试下单成功
- [ ] 日志正常输出

---

完成以上步骤后，你的代理服务器就可以正常使用了！🎉
