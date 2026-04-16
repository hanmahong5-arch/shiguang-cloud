# shiguang-control API 契约

> 版本: 2026-04-15 · 无 OpenAPI spec（待生成）· 错误格式: RFC 7807

---

## 基础

| 属性 | 值 |
|------|-----|
| 默认端口 | `10443` |
| 协议 | HTTP（开发）/ HTTPS（生产） |
| Content-Type | `application/json` |
| 错误 Content-Type | `application/problem+json` (RFC 7807) |
| 认证 | JWT Bearer（仅 `/api/admin/*`） |
| Request ID | 每请求自动分配 `X-Request-Id` 响应头 |

---

## 端点清单

### 运维探针

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/readyz` | — | 轻量存活检查，始终 200 `ok` |
| GET | `/livez` | — | `/readyz` 别名（K8s 约定） |
| GET | `/healthz` | — | 深度健康检查（DB ping + gate fetch），200/503 |
| GET | `/metrics` | — | Prometheus text/plain 计数器（仅 loopback） |

### 账号 (`/api/account/*`)

| 方法 | 路径 | 认证 | Body | 响应 |
|------|------|------|------|------|
| POST | `/api/account/register` | — | `{server, name, password, email}` | `{ok: true}` / 409 / 429 |
| POST | `/api/account/login` | — | `{server, name, password}` | `{ok: true, session_token}` / 401 / 429 |
| POST | `/api/account/change_password` | — | `{server, name, old_password, new_password}` | `{ok: true}` / 401 |
| POST | `/api/account/reset_password` | — | `{server, name, email}` | `{ok: true, new_password}` / 403 |

`server` 字段取值：`"5.8"` / `"4.8"`（或 `"58"` / `"48"`）。

### Token Handoff (`/api/token/*`)

| 方法 | 路径 | 认证 | Body | 响应 |
|------|------|------|------|------|
| POST | `/api/token/validate` | loopback IP 白名单 | `{token}` | `{ok, account, server}` / 401 / 403 |

**安全约束**：仅接受 `127.0.0.1` / `::1` 来源；非 loopback 返回 403 Problem。

### ExternalAuth 桥接

| 方法 | 路径 | 认证 | Body | 响应 |
|------|------|------|------|------|
| POST | `/api/external-auth` | — | `{user, password}` | `{accountId, aionAuthResponseId}` |

兼容 Beyond 4.8 `ExternalAuth.java` 接口。密码以 `SG-` 开头时走 Token Handoff。

### Launcher (`/api/launcher/*`)

| 方法 | 路径 | 认证 | 响应 |
|------|------|------|------|
| GET | `/api/launcher/config` | — | `{public_gate_ip, patch_manifest_url, news_url, servers[]}` |

### Admin (`/api/admin/*`)

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| POST | `/api/admin/login` | — | 签发 JWT |
| GET | `/api/admin/me` | JWT | 当前管理员身份 |
| ... | `/api/admin/gates/*` | JWT | gate 管理（ban/unban/kick） |

### WebSocket

| 路径 | 参数 | 说明 |
|------|------|------|
| `/ws` | `?account=xxx&server=5.8` | launcher 长连接，推送在线人数 |

---

## 错误格式 (RFC 7807)

```json
{
  "type": "about:blank",
  "status": 403,
  "title": "Forbidden",
  "detail": "token validation restricted to loopback"
}
```

`/api/token/validate` 端点已迁移至 RFC 7807。其余端点渐进迁移中。

---

## Wails 事件协议

launcher 前端通过 Wails runtime 接收以下事件：

| 事件名 | Payload | 触发时机 |
|--------|---------|---------|
| `brand:loaded` | `BrandConfig` | 品牌加载/刷新成功 |
| `brand:cleared` | — | 服务器码清除 |
| `patch:progress` | `{phase, done, total, file}` | 补丁下载/校验进度 |
| `patch:complete` | `{ok: true}` | 补丁完成 |
| `patch:error` | `{error}` | 补丁失败 |
| `control:online_count` | JSON string `{"5.8": N, "4.8": M}` | 在线人数更新 |
| `control:offline` | `{error}` | control 不可达（重试用完） |
| `control:online` | — | control 恢复连接 |

---

## Prometheus 指标

所有指标前缀 `sgcontrol_`，类型均为 counter：

| 指标名 | 说明 |
|--------|------|
| `sgcontrol_tokens_issued_total` | session token 签发总数 |
| `sgcontrol_tokens_consumed_total` | token 成功消费 |
| `sgcontrol_tokens_rejected_total` | token 验证失败 |
| `sgcontrol_tokens_blocked_total` | loopback 白名单拦截 |
| `sgcontrol_logins_ok_total` | 密码登录成功 |
| `sgcontrol_logins_failed_total` | 密码登录失败 |
| `sgcontrol_registers_ok_total` | 注册成功 |
| `sgcontrol_registers_failed_total` | 注册失败 |
| `sgcontrol_external_auth_ok_total` | ExternalAuth 成功 |
| `sgcontrol_external_auth_failed_total` | ExternalAuth 失败 |
| `sgcontrol_requests_total` | HTTP 请求总数 |
| `sgcontrol_requests_4xx_total` | 4xx 响应 |
| `sgcontrol_requests_5xx_total` | 5xx 响应 |

---

## Security Headers

所有响应自动附加：
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: camera=(), microphone=(), geolocation=()`
- `Strict-Transport-Security: max-age=63072000; includeSubDomains`（仅 TLS）
