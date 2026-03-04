# AGENTS.md

## 项目概览

本仓库是一个币安合约交易工具，采用前后端分离架构：

- 后端：Go（主程序入口 `main.go`，模块名 `tools`）
- 移动端：Expo React Native（目录 `trading-app/`）
- 代理服务：Go 代理（目录 `cmd/proxy_server/`）

本说明基于仓库内以下文件推断命令：

- `makefile`
- `go.mod`
- `.github/workflows/ci.yml`
- `trading-app/package.json`
- `trading-app/scripts/lint.mjs`
- `cmd/proxy_server/README.md`

未发现根目录 `README`、`docker-compose*`、`Caddyfile`。

## 安装依赖

后端（Go）：

```bash
cd /home/wws/tool_ws
go mod download
```

前端（Expo App）：

```bash
cd /home/wws/tool_ws/trading-app
npm ci
```

## 启动开发与构建

后端开发启动：

```bash
cd /home/wws/tool_ws
make run
```

后端仅执行数据库迁移：

```bash
cd /home/wws/tool_ws
make migrate
```

后端构建（Linux amd64，输出 `./tool`）：

```bash
cd /home/wws/tool_ws
make build
```

代理服务构建（输出 `./proxy`）：

```bash
cd /home/wws/tool_ws
make build-proxy
```

移动端开发启动：

```bash
cd /home/wws/tool_ws/trading-app
npm run start
```

移动端快捷启动：

```bash
cd /home/wws/tool_ws/trading-app
npm run android
npm run ios
npm run web
```

移动端构建（EAS，本地 Android preview）：

```bash
cd /home/wws/tool_ws/trading-app
./build.sh
```

## 测试与 Lint

后端测试（CI 同款）：

```bash
cd /home/wws/tool_ws
go test ./...
```

前端 lint：

```bash
cd /home/wws/tool_ws/trading-app
npm run lint
```

前端测试（当前脚本与 lint 相同）：

```bash
cd /home/wws/tool_ws/trading-app
npm run test
```

后端 lint：未发现后端 lint 命令（仓库中无 `make lint` / `golangci-lint` 配置）。

## 常用目录说明

- `main.go`：后端服务入口
- `api/`：后端核心业务、HTTP Handler、策略与风控
- `websocket/`：币安 WebSocket 相关封装
- `agent/`：LLM Agent 相关逻辑
- `cmd/proxy_server/`：独立代理服务
- `scripts/`：运维/部署脚本（如代理部署脚本）
- `trading-app/`：Expo React Native 客户端
- `trading-app/src/`：前端业务代码与组件
- `docs/`：项目文档与补充说明
- `build/`：构建相关目录（当前仓库内为空）

## 代码风格与约束

- Go 改动遵循 `gofmt` 风格，保持 import 与格式一致。
- 前端 `lint` 脚本会检查：
  - 是否存在 Git 冲突标记（`<<<<<<<` / `=======` / `>>>>>>>`）
  - 是否存在行尾空白
- 非必要不要修改或提交以下生成物目录/文件：
  - `tool`
  - `trading-app/node_modules/`
  - `trading-app/.expo/`
  - `trading-app/dist/`
  - `cmd/proxy_server/proxy_server`
- 不要提交敏感配置或密钥文件：
  - `config.json`
  - `*.pem`
- `make main` 会执行 `scp/ssh` 到远程机器 `wws` 并重启服务；非部署场景不要执行。

## 提交前检查清单

- [ ] 后端测试通过：`cd /home/wws/tool_ws && go test ./...`
- [ ] 前端 lint 通过：`cd /home/wws/tool_ws/trading-app && npm run lint`
- [ ] 前端测试通过：`cd /home/wws/tool_ws/trading-app && npm run test`
- [ ] 如涉及构建变更，至少执行一次构建：
  - 后端：`cd /home/wws/tool_ws && make build`
  - 前端：`cd /home/wws/tool_ws/trading-app && ./build.sh`
- [ ] `git status` 确认未误提交生成物/敏感文件（`config.json`、`*.pem`、`node_modules`、`dist`、二进制文件等）
