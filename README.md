# ShiguangCloud 拾光云

Multi-tenant SaaS platform for AION private server operators. Hub-and-Spoke architecture: cloud Hub manages operators, on-prem Agent handles game traffic. Full Go + gRPC + Wails + React stack.

多租户 SaaS 平台，面向 AION 私服运营商。Hub-Spoke 架构：云端 Hub 管理运营商，本地 Agent 处理游戏流量。全 Go + gRPC + Wails + React 技术栈。

---

## 架构 / Architecture

```
玩家机器                            运营服务器
┌──────────────────┐              ┌──────────────────────────────┐
│ shiguang-launcher│              │  shiguang-gate-58 (5.8 线)   │
│   Wails v2       │   TCP ───────▶   :2108 auth → 127.0.0.1    │
│   React+TS UI    │              │   :7777 world → 127.0.0.1   │
│   Go backend     │              │   PROXY v2 → AionCore        │
└────────┬─────────┘              └──────────────────────────────┘
         │ WSS:10443              ┌──────────────────────────────┐
         │ (账号/配置/心跳)        │  shiguang-gate-48 (4.8 线)   │
         ▼                        │   :2107 LS  → 127.0.0.1     │
┌──────────────────┐              │   :7778 GS  → 127.0.0.1     │
│ shiguang-control │              │   :10241 CS → 127.0.0.1     │
│  Fiber + WSS     │              │   PROXY v2 → Beyond          │
│  pgx/v5 → PG     │              └──────────────────────────────┘
│  内嵌 Admin SPA  │
└──────────────────┘
```

---

## 组件 / Components

| 组件 | 技术 | 路径 | 说明 |
|------|------|------|------|
| shiguang-hub | Go + Fiber + gRPC + pgx | `shiguang-hub/` | 云端多租户中心 (27MB) + Vite React 运营商仪表盘 (77KB gzipped, 6 pages, glassmorphism dark theme) |
| shiguang-agent | Go + errgroup | `shiguang-agent/` | 本地统一二进制 (29MB) = gate + control + hubconn |
| shiguang-launcher | Wails v2 + Go + React | `shiguang-launcher/` | 玩家端白标启动器 (11.5MB)，运行时品牌切换 |
| shiguang-control | Go + Fiber + pgx | `shiguang-control/` | REST/WSS 控制中心 + Token Handoff |
| shiguang-gate | Go + stdlib net | `shiguang-gate/` | 透明 TCP 代理 + PROXY Protocol v2 (9MB) |
| shared | Go module | `shared/` | NCSoft hash + SHA1 + pgx + protobuf + chunker |
| patchbuilder | Go CLI | `shared/cmd/patchbuilder/` | 4MB 块级补丁 manifest 生成器 (3.2MB) |

---

## 一键构建 / One-click Build

```powershell
cd tools\ShiguangSuite
powershell -ExecutionPolicy Bypass -File build.ps1
```

输出在 `release/`：
- `shiguang-gate.exe` — 双版本共用，用不同 `-config` 启动
- `shiguang-control.exe` + `web/dist/`
- `shiguang-launcher.exe`
- 各种 YAML 配置模板

---

## 部署前置条件 / Deployment Prerequisites

⚠️ **必须先修改游戏服务端**，本系统依赖 PROXY Protocol v2：

### AionCore 5.8 (C++)
```bash
# 启动 auth-server 时设置 env var
set AUTH_EXPECT_PROXY_V2=1
./aioncore-auth.exe

# world-server: 在 etc/world.xml (或对应配置) 添加
# <expect_proxy_v2>true</expect_proxy_v2>
```

### Beyond 4.8 (Java)
在 `config/network.properties` 添加：
```properties
loginserver.network.client.expect_proxy_v2=true
gameserver.network.client.expect_proxy_v2=true
```

**不打此补丁的服务端启动后会立即拒绝所有通过 gate 的连接**（因为握手时序不对）。

---

## 部署流程 / Deploy

```bash
# 1. 修改 release/control.yaml：
#    - jwt.secret → 32+ 位随机字符串
#    - launcher.public_gate_ip → gate 公网 IP
#    - db_58 / db_48 → PostgreSQL 连接串

# 2. 启动 gate（两个独立进程）
release\shiguang-gate.exe -config gate-58.yaml
release\shiguang-gate.exe -config gate-48.yaml

# 3. 启动 control
set SHIGUANG_ADMIN_PASS=your_strong_password
release\shiguang-control.exe -config control.yaml -web web\dist

# 4. 浏览器访问 https://control.yourdomain.com:10443/admin/index.html
# 5. 分发 shiguang-launcher.exe 给玩家
```

---

## 相比 AionNetGate 的改进 / Improvements over AionNetGate

| 问题 | 老系统 | 本套件 |
|------|--------|--------|
| SQL 注入 | string.Format 拼接 | pgx 参数化查询 |
| 传输加密 | XOR `'煌'` 单字节 | TLS 1.3 控制通道 |
| 真实客户端 IP | 丢失（服务端看到 127.0.0.1） | PROXY Protocol v2 恢复 |
| 竞态 | 无锁 Dictionary | sync.Map + atomic + context.CancelFunc |
| 管理界面 | WinForms 桌面端 | 浏览器 React SPA |
| 反外挂 | 分裂（launcher 扫描易绕过） | 复用 `aioncore/ac-server` |
| 故障域 | 单进程 | gate 按版本拆分独立进程 |
| .NET 版本 | 2.0 (2005 年) | — （纯 Go + WebView2） |
| 启动器大小 | 2MB + .NET Framework | 11MB 单文件 (UPX 可进一步压缩) |

---

## 重要安全声明 / Security Notes

- **L4 DDoS 防护**: shiguang-gate 只提供 L7 层限速和 IP 封禁。真正的 SYN Flood 防护必须在内核层（iptables synproxy）或上游 CDN（Cloudflare / 阿里云高防）实现。**Go 的 net.Accept() 在 TCP 握手已完成后才收到连接，对 SYN flood 无能为力。**
- **TLS 证书**: 生产环境必须配置真实证书（Let's Encrypt 或商业证书）。开发模式的 HTTP 监听仅用于本地测试。
- **JWT secret**: 至少 32 个随机字符，配置在 `control.yaml` 或通过 `SHIGUANG_JWT_SECRET` 环境变量注入。
- **升级策略**: MMO 长连接场景不使用 tableflip 平滑重启（会堆积僵尸进程）。采用计划停服维护为主 + 30 分钟硬超时踢线为兜底。

---

## 测试覆盖 / Test Coverage

| 模块 | 测试数 | 说明 |
|------|--------|------|
| shared/crypto | 5 | NCSoft hash 与 C# 字节级对齐 (15 参考向量), SHA1 (7 向量) |
| shiguang-gate/proxy | 5+4 | PROXY v2 编码器 + TCP relay |
| shiguang-gate/defense | 9 | 限速 + 封禁列表 + 原子持久化 |
| shiguang-gate/config | 5 | YAML 加载 + 验证 |
| shiguang-gate/api | 5 | HTTP 管理端点 |
| shiguang-control/config | 4 | YAML 加载 |
| shiguang-control/middleware | 5 | JWT 签发 + 校验 |
| shiguang-control/hub | 4 | WSS hub 广播/踢人 |
| shiguang-control/handlers | 4 | Admin API + fan-out |
| shiguang-launcher/patching | 3 | MD5 校验 + 断点续传 + chunk fallback |
| shiguang-launcher/game | 4 | 参数构建 + Token Handoff 标记 |

| shared/chunker | 5 | 4MB 块分割 + SHA-256 + manifest 序列化 + diff |

**总计：62 个单元测试**

---

## API 契约 / API Contract

完整端点清单、请求/响应 schema、RFC 7807 错误格式、Prometheus 指标定义、Wails 事件协议详见：

**[`doc/architecture/control-api.md`](doc/architecture/control-api.md)**

---

## 可观测性 / Observability

| 维度 | 实现 |
|------|------|
| **Request ID** | 每请求自动分配 UUID，写入 `X-Request-Id` 响应头 |
| **Prometheus** | `GET /metrics` — 13 个 `sgcontrol_*` 计数器（tokens issued/consumed/rejected、logins、registers、external-auth、HTTP 4xx/5xx） |
| **健康探针** | `GET /readyz` (liveness) · `GET /livez` (alias) · `GET /healthz` (deep: DB ping + gate fetch) |
| **日志** | 请求级 Fiber logger（status / latency / IP / method / path） |

---

## 安全加固 / Security Hardening

| 维度 | 实现 |
|------|------|
| **Security Headers** | `X-Content-Type-Options: nosniff` · `X-Frame-Options: DENY` · `X-XSS-Protection` · `Referrer-Policy` · `Permissions-Policy` · HSTS (TLS only) |
| **Token Handoff** | `/api/token/validate` loopback IP 白名单（非 127.0.0.1/::1 → 403 Problem） |
| **Account 白名单** | `TokenHandoff.java` 正则 `^[A-Za-z0-9_-]{4,32}$` 兜底，防 SQL/日志注入 |
| **PROXY v2** | LOCAL command (0x20) 支持（健康探针）；超时可配 `-Daion.proxy_v2.read_timeout_ms` |
| **错误格式** | RFC 7807 `application/problem+json`（渐进迁移中） |
| **Session 文件** | `.sg-session` 写入 `0600` + Windows 隐藏属性 |

---

## Launcher UI/UX

| 组件 | 说明 |
|------|------|
| **ErrorBoundary** | 渲染异常恢复 UI（错误详情可折叠，一键重新加载） |
| **Toast** | 底部浮层通知（success / error / warning / info），自动 TTL + FIFO + 去重 |
| **ConfirmDialog** | Promise 式模态确认框（Esc / Enter / 背景点击），danger 红色变体 |
| **FormField** | 统一表单字段（label / input / error / hint / required），WAI-ARIA 无障碍 |
| **NetworkBanner** | 控制中心离线黄色横幅，15s 自动重试 + 手动重试按钮 |
| **Spinner** | CSS keyframe 旋转，尊重 `prefers-reduced-motion` |
| **补丁 ETA** | EMA 平滑速率 + 剩余时间 + 字节量显示 + 斜条纹进度条动画 |
| **字体层级** | Nunito 自托管 woff2 + 6 级 scale（caption → h1） |
| **Validators** | 前端 7 个纯函数校验器，与后端白名单严格对齐 |

操作回退策略：
- 登录失败 → 保留账号、清空密码、自动聚焦密码框
- 注册成功 → 900ms 后自动切到登录 tab 并预填账号
- 改密 → 确认新密码二次输入 + 新密码 ≠ 原密码校验
- 补丁失败 → 错误行 + 重试按钮
- 启动游戏 → 三级前置检查（服务器存在 → 路径已配 → 补丁已检）
- 退出登录 / 切换服务器 → ConfirmDialog danger 模式
- 控制中心 URL → 撤销按钮 + 保存前 URL 校验
- 客户端路径 → 600ms debounce 防抖自动保存

---

## 参考 / References

- AionNetGate 原项目: `tools/AionNetGate/`
- API 契约: [`doc/architecture/control-api.md`](doc/architecture/control-api.md)
- 开发进度: `doc/process.md`
