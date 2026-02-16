#!/bin/bash
# 自动部署脚本 - 在服务器上运行

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== 币安合约 API 代理服务器部署脚本 ===${NC}\n"

# 1. 检查环境变量
echo "1. 检查环境变量..."
if [ -z "$BINANCE_API_KEY" ]; then
    echo -e "${RED}错误: BINANCE_API_KEY 未设置${NC}"
    echo "请运行: export BINANCE_API_KEY=your_key"
    exit 1
fi

if [ -z "$BINANCE_SECRET_KEY" ]; then
    echo -e "${RED}错误: BINANCE_SECRET_KEY 未设置${NC}"
    echo "请运行: export BINANCE_SECRET_KEY=your_secret"
    exit 1
fi

echo -e "${GREEN}✓ 环境变量已配置${NC}"

# 2. 检查 Go 环境
echo -e "\n2. 检查 Go 环境..."
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: Go 未安装${NC}"
    echo "请访问 https://golang.org/dl/ 安装 Go 1.18+"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo -e "${GREEN}✓ Go 已安装: $GO_VERSION${NC}"

# 3. 检查项目目录
echo -e "\n3. 检查项目目录..."
PROJECT_DIR="/opt/tool_ws"
if [ ! -d "$PROJECT_DIR" ]; then
    echo -e "${YELLOW}项目目录不存在，正在创建...${NC}"
    sudo mkdir -p $PROJECT_DIR
    sudo chown $USER:$USER $PROJECT_DIR
fi
cd $PROJECT_DIR
echo -e "${GREEN}✓ 项目目录: $PROJECT_DIR${NC}"

# 4. 拉取代码（如果是 Git 仓库）
echo -e "\n4. 更新代码..."
if [ -d ".git" ]; then
    git pull
    echo -e "${GREEN}✓ 代码已更新${NC}"
else
    echo -e "${YELLOW}! 不是 Git 仓库，跳过更新${NC}"
fi

# 5. 下载依赖
echo -e "\n5. 下载依赖..."
go mod download
echo -e "${GREEN}✓ 依赖已下载${NC}"

# 6. 编译代理服务器
echo -e "\n6. 编译代理服务器..."
go build -o proxy_server cmd/proxy_server/main.go
chmod +x proxy_server
echo -e "${GREEN}✓ 编译成功: proxy_server${NC}"

# 7. 创建 systemd 服务文件
echo -e "\n7. 创建 systemd 服务..."
SERVICE_FILE="/etc/systemd/system/binance-proxy.service"

sudo bash -c "cat > $SERVICE_FILE" << EOF
[Unit]
Description=Binance Futures API Proxy Server
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$PROJECT_DIR
Environment="BINANCE_API_KEY=$BINANCE_API_KEY"
Environment="BINANCE_SECRET_KEY=$BINANCE_SECRET_KEY"
Environment="BINANCE_TESTNET=${BINANCE_TESTNET:-false}"
ExecStart=$PROJECT_DIR/proxy_server
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}✓ systemd 服务文件已创建${NC}"

# 8. 重载 systemd
echo -e "\n8. 重载 systemd..."
sudo systemctl daemon-reload
echo -e "${GREEN}✓ systemd 已重载${NC}"

# 9. 停止旧服务（如果存在）
echo -e "\n9. 停止旧服务..."
sudo systemctl stop binance-proxy 2>/dev/null || echo -e "${YELLOW}! 旧服务不存在，跳过${NC}"

# 10. 启动服务
echo -e "\n10. 启动代理服务..."
sudo systemctl enable binance-proxy
sudo systemctl start binance-proxy

# 等待服务启动
sleep 2

# 11. 检查服务状态
echo -e "\n11. 检查服务状态..."
if sudo systemctl is-active --quiet binance-proxy; then
    echo -e "${GREEN}✓ 服务运行中${NC}"
    sudo systemctl status binance-proxy --no-pager | head -15
else
    echo -e "${RED}✗ 服务启动失败${NC}"
    echo -e "\n查看日志:"
    sudo journalctl -u binance-proxy -n 50 --no-pager
    exit 1
fi

# 12. 健康检查
echo -e "\n12. 健康检查..."
sleep 1
HEALTH_CHECK=$(curl -s http://localhost:8888/health)
if echo "$HEALTH_CHECK" | grep -q "ok"; then
    echo -e "${GREEN}✓ 健康检查通过${NC}"
    echo "响应: $HEALTH_CHECK"
else
    echo -e "${RED}✗ 健康检查失败${NC}"
    exit 1
fi

# 13. 显示服务信息
echo -e "\n${GREEN}=== 部署成功 ===${NC}"
echo -e "\n服务信息:"
echo "  端口: 8888"
echo "  服务名: binance-proxy"
echo ""
echo "管理命令:"
echo "  查看状态: sudo systemctl status binance-proxy"
echo "  查看日志: sudo journalctl -u binance-proxy -f"
echo "  重启服务: sudo systemctl restart binance-proxy"
echo "  停止服务: sudo systemctl stop binance-proxy"
echo ""
echo "测试命令:"
echo "  curl http://localhost:8888/health"
echo "  curl http://localhost:8888/api/positions"
echo ""
echo -e "${YELLOW}注意: 请配置防火墙以限制访问端口 8888${NC}"
echo ""

# 14. 获取服务器 IP
SERVER_IP=$(hostname -I | awk '{print $1}')
echo "服务器 IP: $SERVER_IP"
echo -e "\n本地客户端连接地址:"
echo -e "${GREEN}  client := api.NewProxyClient(\"http://$SERVER_IP:8888\")${NC}"
