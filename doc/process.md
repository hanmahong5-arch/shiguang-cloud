# ShiguangSuite Development Progress / 开发进度

## v1 Single-Tenant Tools (COMPLETE) / v1 单租户工具（完成）

### shared/crypto — NCSoft + SHA1 password hashing / 密码哈希
- **Need**: Game client password verification for both 5.8 and 4.8 lines
- **Method**: Ported NCSoft hash (C# → Go, byte-parity verified) + SHA1+Base64 for Beyond 4.8
- **Changes**: `shared/crypto/ncsoft.go`, `shared/crypto/sha1_base64.go`, `shared/crypto/ncsoft_test.go`
- **Result**: 15 test vectors pass, byte-identical to original C# implementation

### shiguang-gate — TCP relay + defense + admin API / TCP 中继 + 防御 + 管理 API
- **Need**: L4 proxy between game clients and servers with DDoS protection
- **Method**: Go TCP relay with PROXY Protocol v2, per-IP rate limiting, banlist with atomic persistence
- **Changes**: `shiguang-gate/internal/{proxy,defense,api,config}/`, `shiguang-gate/cmd/gate/main.go`
- **Result**: 8.5MB binary, 28 tests passing, PROXY v2 headers for real client IP preservation

### shiguang-control — Account API + WSS hub / 账号 API + WebSocket 中心
- **Need**: Player account management and real-time launcher communication
- **Method**: Fiber REST (register/login/change/reset), JWT admin auth, WebSocket hub with CSP channels
- **Changes**: `shiguang-control/internal/{handlers,hub,service,middleware,config}/`, `shiguang-control/cmd/control/main.go`
- **Result**: 19MB binary, 17 tests passing

### shiguang-launcher — Wails v2 desktop app / 桌面启动器
- **Need**: Player-facing game launcher with patching and server selection
- **Method**: Wails v2 (Go backend + React frontend), WebSocket reconnect, chunk patcher
- **Changes**: `shiguang-launcher/{app.go,main.go,internal/,frontend/}`
- **Result**: 11.5MB Wails binary, 7 tests passing

### AionCore C++ PROXY v2 patches / PROXY v2 C++ 补丁
- **Need**: Game server reads real client IP from PROXY Protocol v2 headers
- **Method**: Blocking pre-read of PROXY v2 header before async `on_connect` (respects S_INIT timing)
- **Changes**: `aioncore/shared/network/tcp_server.{h,cpp}`, `aioncore/shared/server/server_application.{h,cpp}`, `aioncore/auth-server/main.cpp`, `aioncore/world-server/server_config.h`
- **Result**: Compiled, tested with gate relay

### Beyond 4.8 Java PROXY v2 patches / PROXY v2 Java 补丁
- **Need**: Java game server reads real client IP from PROXY Protocol v2 headers
- **Method**: Blocking pre-read with `configureBlocking(true)` + `setSoTimeout(5000)` before NIO
- **Changes**: `ProxyProtocolV2.java`, `ServerCfg.java`, `Acceptor.java`, `AConnection.java`, `NioServer.java`, `GameServer.java`, `NetworkConfig.java`, `Config.java`, `NetConnector.java`
- **Result**: Compiled, tested with gate relay

---

## v2 Multi-Tenant SaaS (IN PROGRESS) / v2 多租户 SaaS（进行中）

### Phase C-0: Multi-tenant data layer (COMPLETE) / 多租户数据层（完成）
- **Need**: Shared database schema for all tenants with row-level isolation
- **Method**: PostgreSQL DDL with pgcrypto UUIDs, pgx parameterized CRUD
- **Changes**: `shared/tenant/{model.go,schema.sql,repo.go,hub_protocol.go}`, `proto/hub.proto`
- **Result**: 8 entity structs, 21 CRUD methods, gRPC service definition

### Phase C-1: Agent + Hub scaffolding (COMPLETE) / Agent + Hub 骨架（完成）
- **Need**: Unified agent binary (gate + control) + cloud hub REST API
- **Method**: pkg/embed facade pattern + errgroup lifecycle, Fiber REST + JWT auth
- **Changes**: `shiguang-gate/pkg/embed/gate.go`, `shiguang-control/pkg/embed/control.go`, `shiguang-agent/cmd/agent/main.go`, `shiguang-hub/{cmd/hub/main.go,handlers/,hubconfig/}`
- **Result**: Agent 20.5MB (3 subsystems), Hub 18.7MB (tenant CRUD + bootstrap)

### Phase C-2: Launcher white-label (COMPLETE) / 启动器白标（完成）
- **Need**: One launcher binary serves all operators via invite code → brand download
- **Method**: CSS variables + overlay asset handler + brand cache persistence
- **Changes**: `shiguang-launcher/{app.go,main.go,frontend/src/{App.tsx,style.css}}`
- **Result**: ActivationScreen + runtime brand loading + local cache

### Architecture Review Fixes (COMPLETE 2026-04-12) / 架构审查修复（完成）
- **P0**: ServerLine type consolidated from 3 definitions to 1 (`shared/tenant/wire.go`)
  - **Changes**: `shared/tenant/wire.go` (new), `shiguang-control/internal/config/config.go`, `shiguang-control/internal/handlers/launcher.go`, `shiguang-control/pkg/embed/control.go`, `shiguang-launcher/internal/control/wsclient.go`, `shiguang-agent/cmd/agent/main.go`
- **P1a**: Removed `ControlInstance.FiberApp()` (over-exposed Fiber internals)
  - **Changes**: `shiguang-control/pkg/embed/control.go` (deleted method + toLauncherConfig + bridge types)
- **P1b**: Added `pg_advisory_xact_lock` to `CreateServerLine` (TOCTOU race fix)
  - **Changes**: `shared/tenant/repo.go` (wrapped in transaction with advisory lock)

### Phase C-3: gRPC heartbeat system (COMPLETE 2026-04-12) / gRPC 心跳系统（完成）
- **Need**: Agent ↔ Hub bidirectional gRPC stream for heartbeats, metrics, commands
- **Method**: protobuf compilation, bidirectional stream with jitter+exponential backoff reconnect, Hub-side rate limiting (100 conn/s token bucket)
- **Changes**:
  - `shared/hubpb/hub.pb.go`, `shared/hubpb/hub_grpc.pb.go` (protoc generated, 1705 lines)
  - `shiguang-agent/internal/hubconn/client.go` (new: gRPC client with jitter heartbeat + command dispatch)
  - `shiguang-hub/internal/grpcserver/server.go` (new: gRPC server with auth + heartbeat + FetchConfig + PushCommand + rate limiting)
  - `shiguang-agent/cmd/agent/main.go` (replaced placeholder with real hubconn, gate stats → heartbeat, hub commands → banlist)
  - `shiguang-hub/cmd/hub/main.go` (integrated gRPC server with graceful shutdown)
- **Result**: Agent 29MB (gRPC +8.5MB), Hub 27MB (gRPC +8.3MB), 57 tests all passing
- **Features**:
  - Heartbeat: 10s base + 0-3s jitter, relay stats + ban count
  - Reconnect: exponential backoff 1s→30s + 0-5s jitter (prevents thundering herd)
  - Hub rate limit: 100 new conn/s token bucket interceptor
  - Command dispatch: Hub→Agent ban/unban/kick/config/announcement via stream
  - FetchConfig: one-shot agent config pull (tenant + lines + branding)
  - PushCommand: REST→gRPC bridge for operator dashboard

---

## Binary Summary / 二进制汇总

| Binary | Size | Tests | Status |
|--------|------|-------|--------|
| shiguang-gate.exe | 8.5MB | 28 | v1 complete |
| shiguang-control.exe | 19MB | 17 | v1 complete |
| shiguang-launcher.exe | 11.5MB | 7 | v2 C-2 complete |
| shiguang-agent.exe | 29MB | — | v2 C-3 complete |
| shiguang-hub.exe | 27MB | — | v2 C-3 complete |
| **Total** | **94.5MB** | **57** | — |

### Phase C-4: Operator Dashboard SPA (COMPLETE 2026-04-12) / 运营商仪表盘（完成）
- **Need**: Operator-facing web dashboard for managing their private server
- **Method**: Vite + React 18 + react-router-dom 6, dark theme matching launcher aesthetic
- **Changes**:
  - `shiguang-hub/admin-spa/` (new Vite project: ~500 lines JSX + CSS)
  - Pages: Login, Dashboard (agent status + stats), Server Lines (CRUD), Branding (theme editor + preview), Invite Codes, Gate Agents (15s auto-refresh)
  - `shiguang-hub/handlers/tenant.go` (fixed login by email, added listAgents, listCodes)
  - `shared/tenant/repo.go` (added GetTenantByEmail, ListGateAgents, ListTenantCodes)
  - `shared/tenant/hub_protocol.go` (removed 9 proto-superseded Go types, kept REST wire types)
- **Result**: 72KB gzipped SPA, served from Hub at /admin, all API endpoints wired

### Phase C-5: Token Handoff + Chunk Patching (COMPLETE 2026-04-12) / Token切换 + 块补丁（完成）
- **Need**: End-to-end player authentication (launcher → game server) + efficient client patching
- **Method**:
  - Token Handoff: in-memory token store (5min TTL, single-use) in Control, session_token returned on login, written to `.sg-session` file for version.dll, validation endpoint for game server
  - Chunk Patching: 4MB fixed-size content-addressable chunks, SHA-256 hash, streaming manifest decode
- **Changes**:
  - `shiguang-control/internal/service/tokenstore.go` (new: in-memory token store with TTL + cleanup)
  - `shiguang-control/internal/handlers/account.go` (login returns session_token, POST /api/token/validate)
  - `shiguang-launcher/internal/game/starter.go` (SessionToken field, writes .sg-session file)
  - `shiguang-launcher/internal/control/wsclient.go` (Login returns session token)
  - `shiguang-launcher/app.go` (captures + passes session token to game.Start)
  - `shared/chunker/chunker.go` (new: 4MB chunk splitting + SHA-256)
  - `shared/chunker/manifest.go` (new: streaming JSON manifest + diff)
  - `shared/chunker/chunker_test.go` (new: 5 tests)
- **Result**: Full Token Handoff flow implemented, chunker with 5 tests passing

## Binary Summary / 二进制汇总

| Binary | Size | Tests | Status |
|--------|------|-------|--------|
| shiguang-gate.exe | 9MB | 28 | v1 complete |
| shiguang-control.exe | 20MB | 17 | v2 C-5 complete |
| shiguang-launcher.exe | 11.5MB | 7 | v2 C-5 complete |
| shiguang-agent.exe | 29MB | — | v2 C-3 complete |
| shiguang-hub.exe | 27MB | — | v2 C-4 complete |
| **Total** | **96.5MB** | **62** | — |

### Admin SPA v2 Redesign (COMPLETE 2026-04-12) / 运营商仪表盘重设计（完成）
- **Need**: Production-grade operator dashboard with professional design quality
- **Method**: Complete SPA rewrite — modular architecture, glassmorphism design system, lucide-react icons, toast notifications, skeleton loading, animated background orbs
- **Changes**:
  - `shiguang-hub/admin-spa/src/styles.css` (rewritten: 520-line design system with CSS animations, custom scrollbar, glassmorphism, responsive breakpoints)
  - `shiguang-hub/admin-spa/src/App.jsx` (rewritten: auth provider + layout shell + sidebar navigation with section labels)
  - `shiguang-hub/admin-spa/src/api.js` (enhanced: better error handling, empty response support)
  - `shiguang-hub/admin-spa/src/hooks/useAuth.jsx` (new: React Context auth provider with loading state)
  - `shiguang-hub/admin-spa/src/hooks/useToast.jsx` (new: toast notification system with slide-in/out animation)
  - `shiguang-hub/admin-spa/src/components.jsx` (new: ToastContainer, Skeleton, EmptyState, CopyButton, ConfirmModal, timeAgo, formatDate)
  - `shiguang-hub/admin-spa/src/pages/Login.jsx` (new: centered glassmorphic login with Shield icon branding)
  - `shiguang-hub/admin-spa/src/pages/Dashboard.jsx` (new: stat cards with gradient borders + agent status table + quick actions grid + account details)
  - `shiguang-hub/admin-spa/src/pages/Servers.jsx` (new: version badges v5.8/v4.8, smart port presets, slide-open create form)
  - `shiguang-hub/admin-spa/src/pages/Branding.jsx` (new: split layout form+preview, native color pickers, live launcher mockup)
  - `shiguang-hub/admin-spa/src/pages/Codes.jsx` (new: code cards with monospace display, copy-to-clipboard, input validation)
  - `shiguang-hub/admin-spa/src/pages/Agents.jsx` (new: card grid with pulse status dots, auto-refresh countdown, admin port external links)
- **Result**: 77KB gzipped SPA (19KB CSS + 249KB JS), 12 source files, 6 pages, Playwright-verified screenshots

### version.dll Token Handoff Reader (COMPLETE 2026-04-12) / Token 切换读取器（完成）
- **Need**: Bridge Token Handoff from launcher to game client via version.dll
- **Method**: Read `.sg-session` file at DLL_PROCESS_ATTACH, delete after read (single-use), hook send() for future auth packet injection
- **Changes**:
  - `tools/version-dll/src/AionVersionDll/version.cpp` (added: `s_sessionToken`/`s_hasSessionToken` globals, `LoadSessionToken()` function resolves client root from DLL path, `zzsend()` hook stub, conditional `-sg-token-handoff` flag)
  - `shiguang-launcher/internal/game/starter.go` (added: `-sg-token-handoff` command-line arg when session token present)
- **Result**: Full launcher→version.dll→game client token pipeline wired. Protocol-level packet injection deferred to integration testing.

### Chunk-Aware Parallel Patcher (COMPLETE 2026-04-12) / 分块并行补丁器（完成）
- **Need**: Efficient delta patching for 10GB+ game clients using 4MB content-addressable chunks
- **Method**: 8-worker parallel download pool, SHA-256 verify before+after write, per-file mutex for offset writes, fallback to legacy file-level patching
- **Changes**:
  - `shiguang-launcher/internal/patching/chunk_patcher.go` (new: 230 lines — `RunChunked()`, `fetchChunkManifest()`, `downloadChunksParallel()`, `downloadAndWriteChunk()`, `verifyWrittenChunks()`)
  - `shiguang-launcher/internal/patching/patcher.go` (modified: `Run()` now tries chunk-based first, renamed legacy to `runLegacy()`)
- **Result**: Patcher now supports both chunk-based (parallel) and file-based (sequential) modes. 62 tests all passing.

### Beyond 4.8 Token Handoff Integration (COMPLETE 2026-04-12) / Beyond 4.8 Token 集成（完成）
- **Need**: End-to-end player authentication from launcher through to game server
- **Method**: Two-layer architecture — Layer 1: ExternalAuth bridge (zero Beyond code changes); Layer 2: Independent TokenHandoff module
- **Design principles**: Independent (separate from ExternalAuth), Decoupled (HTTP-only communication), Reliable (3s connect / 5s read timeout, graceful fallback), Observable (SLF4J structured logging at every decision point), Extensible (config-driven, record-based response)
- **Changes**:
  - Layer 1 — ExternalAuth Bridge (shiguang-control side):
    - `shiguang-control/internal/handlers/account.go` (added: `POST /api/external-auth` endpoint compatible with Beyond's ExternalAuth.java format, dual-mode: normal password validation + SG- token validation)
  - Layer 2 — Independent TokenHandoff Module (Beyond 4.8 side):
    - `login-server/.../utils/TokenHandoff.java` (new: 130-line standalone utility — `isTokenHandoff()`, `extractToken()`, `validate()` with HTTP client, timeout, structured logging, `Result` record)
    - `login-server/.../configs/Config.java` (added: `TOKEN_HANDOFF_URL` property + `useTokenHandoff()`)
    - `login-server/.../clientpackets/CM_LOGIN.java` (modified: 5-line branch — if SG- prefix detected, route to `loginWithToken()`)
    - `login-server/.../controller/AccountController.java` (added: `loginWithToken()` method — validates token via HTTP, loads/auto-creates account, applies all post-login checks identically to normal login)
    - `run/login-server/config/network/network.properties` (added: `loginserver.shiguang.token_handoff.url` config)
- **Result**: Full build+deploy successful. Two deployment modes available.

### Security & Reliability Audit Fixes (COMPLETE 2026-04-12) / 安全与可靠性审计修复（完成）
- **CRITICAL**: Token 溢出修复 — 16字节→12字节 (24hex + "SG-" = 27字符 < 32字节限制), 添加 `-loginex` 标记
- **HIGH**: Windows 文件安全 — `writeSecureFile()` 设置 hidden 属性, 防止 `.sg-session` 被发现
- **HIGH**: TokenStore 持久化 — 原子文件写入 `.sg-tokens.json`, 重启后自动恢复未过期 token
- **HIGH**: 注册/登录速率限制 — per-IP 限速器 (注册 5次/10分钟, 登录 20次/5分钟)
- **MEDIUM**: Branding XSS 防护 — `safeUrl()` 过滤非 http/https 协议 URL
- **MEDIUM**: ExternalAuth 输入验证 — 用户名 64 字符 / 密码 128 字符上限
- **MEDIUM**: Chunk 下载重试 — 每个 chunk 最多 3 次重试 + 指数退避 + jitter

### Engineering Hardening Pass (COMPLETE 2026-04-12) / 工程加固（完成）
- **Need**: Production-grade reliability, observability, and security across all layers
- **Method**: Comprehensive audit of 23 engineering issues → 14 critical fixes implemented
- **Changes**:
  - **Hub graceful shutdown** — `shiguang-control/internal/hub/launcher_hub.go`: Added `done` channel + `Stop()` method. `Run()` now exits cleanly, closing all client connections. `cmd/control/main.go` calls `h.Stop()` before `app.Shutdown()`.
  - **Rich health check** — `shiguang-control/cmd/control/main.go`: `/healthz` now pings both DB pools + probes gate connectivity. Returns 503 with component-level status if any critical dep is down.
  - **Admin login rate limiting** — `shiguang-control/internal/handlers/admin.go`: Added `ipRateLimiter` to `AdminHandler` (10 attempts per IP per 5 minutes), prevents brute-force on admin credentials.
  - **ExternalAuth rate limiting** — `shiguang-control/internal/handlers/account.go`: Added `extAuthRL` (60 req/IP/min) to prevent game server DoS on `/external-auth` endpoint.
  - **ExternalAuth both-line fix** — `account.go`: ExternalAuth now tries both 4.8 and 5.8 service lines sequentially. Account not found on one line → tries next. Wrong password → stops immediately (prevents false login).
  - **Token Handoff audit logging** — `account.go`: Structured `[token-handoff]` and `[external-auth]` log lines at every decision point (issue, accept, reject, not_found). Includes user, IP, server, reason.
  - **CORS restriction** — `cmd/control/main.go`: Replaced `AllowOrigins: "*"` with `AllowOriginsFunc` that only allows localhost/127.0.0.1 origins (admin SPA same-origin + Vite dev server). Server-to-server requests (no Origin header) unaffected.
  - **React Error Boundary** — `admin-spa/src/components.jsx`: Class component catches render errors, shows recovery UI with "Try Again" button instead of white screen crash.
  - **Network Banner** — `admin-spa/src/components.jsx`: Fixed banner appears when browser goes offline. Subscribes to `online`/`offline` events via API layer.
  - **API resilience** — `admin-spa/src/api.js`: Auto-retry on transient errors (network + 5xx, GET only, max 2 retries). 429 rate limit detection with user-friendly message. Network status tracking with subscriber pattern.
  - **Server CRUD complete** — `admin-spa/src/pages/Servers.jsx`: Added edit (inline form reuse) + delete (confirmation modal with impact warning). New API: `updateLine()`, `deleteLine()`.
  - **Code deletion** — `admin-spa/src/pages/Codes.jsx`: Added delete button + confirmation modal. New API: `deleteCode()`.
  - **Dashboard auto-refresh** — `admin-spa/src/pages/Dashboard.jsx`: 30s countdown timer with manual refresh button. Uses `Promise.allSettled` for resilient parallel fetch. Spinning icon during refresh.
  - **ErrorBoundary + NetworkBanner wired** — `admin-spa/src/App.jsx`: ErrorBoundary wraps entire app, NetworkBanner shows above all content.
  - **CSS additions** — `admin-spa/src/styles.css`: Error boundary centered panel, network banner fixed-top with slide-down animation, spin keyframes for refresh icon.
- **Result**: 79KB gzipped SPA. All 11 Go test packages passing. Zero regressions.

### Patch Pipeline Completion + CRUD Gaps (COMPLETE 2026-04-12) / 补丁管道补全 + CRUD 缺口（完成）
- **Need**: Complete the server-side patching pipeline (patchbuilder only generated manifest, not chunk files) + wire missing CRUD backend routes that the SPA frontend already calls
- **Method**: Content-addressable chunk export with atomic writes + Hub static file serving + missing repo/handler CRUD
- **Changes**:
  - **ExportChunks()** — `shared/chunker/chunker.go`: New `ExportChunks(clientRoot, outDir)` function. Builds manifest, writes each chunk to `outDir/chunks/{sha256_hash}`. Content-addressable dedup (skip existing). Atomic write (temp file + rename) prevents partial chunks on crash. Hash verified before write (defense against filesystem corruption during export). Progress logging every 500 chunks. Returns `ExportStats{TotalChunks, NewChunks, SkippedDup, TotalBytes}`.
  - **Patchbuilder CLI rewrite** — `shared/cmd/patchbuilder/main.go`: Now outputs both manifest + chunk files by default (`-out ./patch` creates `patch/chunk-manifest.json` + `patch/chunks/`). Added `-manifest-only` flag for legacy mode. Incremental re-runs only write changed chunks (dedup). Clear "next steps" hint in output.
  - **Hub patch hosting** — `shiguang-hub/cmd/hub/main.go`: Added `/patches/` static file serving from `PatchDir`. 1-hour cache (chunks are immutable). Gzip compression for manifest JSON. Layout: `/patches/{CODE}/chunk-manifest.json` + `/patches/{CODE}/chunks/{hash}`.
  - **Hub config PatchDir** — `shiguang-hub/hubconfig/config.go`: Added `PatchDir string` field. Operators configure `patch_dir: ./patches` in hub.yaml.
  - **Missing repo methods** — `shared/tenant/repo.go`: Added `UpdateServerLine()` (mutable fields only), `DeleteServerLine()` (soft-delete via `enabled=false`, agents get time to detect), `DeleteTenantCode()` (hard delete with tenant_id isolation).
  - **Missing handler routes** — `shiguang-hub/handlers/tenant.go`: Added `PUT /me/lines/:id` (updateLine), `DELETE /me/lines/:id` (deleteLine), `GET /me/theme` (getTheme, returns empty object if none), `DELETE /me/codes/:code` (deleteCode). All enforce tenant_id isolation.
- **Result**: All Go modules build clean. 5/5 chunker tests pass. 79KB SPA builds. Complete patch pipeline: `patchbuilder -client ./client -out ./patch` → copy to Hub `patch_dir` → launcher auto-downloads delta.

### Security & Functional Gaps Round 2 (COMPLETE 2026-04-12) / 安全与功能缺口修复第二轮（完成）
- **Need**: Fix 3 critical gaps found in audit: bootstrap returns empty gate IP (breaks launcher), Hub login unprotected against brute force, Hub CORS still wildcard
- **Method**: Gate agent IP query + per-IP rate limiter + CORS origin restriction
- **Changes**:
  - **Bootstrap gate IP resolution** — `shared/tenant/repo.go`: New `GetOnlineGateIP()` method queries `gate_agents` table for most recently seen online agent (status='online' AND last_seen < 60s). `shiguang-hub/handlers/tenant.go`: Bootstrap handler now calls `GetOnlineGateIP()` instead of returning empty string.
  - **Hub login/register rate limiting** — `shiguang-hub/handlers/tenant.go`: Added `ipRateLimiter` (same implementation as Control). Login: 10 attempts/IP/5min. Register: 3 attempts/IP/10min. Background cleanup every 60s.
  - **Hub CORS restriction** — `shiguang-hub/cmd/hub/main.go`: Replaced `AllowOrigins: "*"` with `AllowOriginsFunc` allowing only localhost/127.0.0.1 origins (admin SPA same-origin + Vite dev server).
  - **ExportChunks test coverage** — `shared/chunker/chunker_test.go`: 2 new tests (TestExportChunks: full pipeline + dedup verification; TestExportChunksAtomicWrite: no leftover .tmp files). Chunker now 7/7 tests. Total project: 67 tests.
- **Result**: All 67 tests passing across 11 packages. Zero regressions. All Go modules build clean.

### Engineering Hardening Pass #2 (COMPLETE 2026-04-12) / 工程加固第二轮（完成）
- **Need**: Fix remaining reliability, security, and feature gaps identified in post-review audit
- **Method**: 6 targeted improvements across all layers — bug fix, reliability, security, features
- **Changes**:
  - **Hubconn backoff reset bug** — `shiguang-agent/internal/hubconn/client.go`: `Run()` never reset exponential backoff after a successful stream. Added time-based detection: if stream was alive >30s before disconnect, backoff resets to 1s (connection was healthy, not a persistent issue). Prevents agents from taking 30s+ to reconnect after transient network blips.
  - **Gate admin graceful shutdown** — `shiguang-gate/internal/api/admin.go`: Replaced `srv.Close()` with `srv.Shutdown(ctx)` using 5-second context deadline. In-flight requests now drain cleanly instead of being dropped. Added `context` import.
  - **Legacy patcher retry** — `shiguang-launcher/internal/patching/patcher.go`: Added 3-attempt retry with exponential backoff + jitter to `downloadFile()`. On flaky networks, transient HTTP failures are retried automatically instead of causing permanent patch failure.
  - **Gate upstream health probing** — `shiguang-gate/internal/proxy/health.go` (new: 65 lines): Background TCP dial prober per relay upstream. Probes every 15s with 3s timeout. After 3 consecutive failures → marks upstream unhealthy. Single success → immediately healthy. `relay.go` uses health status to fast-reject new connections to dead upstreams (saves client from waiting for full dial timeout). Health exposed in admin `/status` response and via heartbeat to Hub.
  - **Chunk manifest HMAC signing** — `shared/chunker/manifest.go`: `WriteManifest()` accepts optional HMAC key → computes HMAC-SHA256 over version+root+sorted chunk hashes → stores signature in manifest. `VerifyManifestSignature()` verifies on download (backwards compatible: unsigned manifests pass). `shared/cmd/patchbuilder/main.go`: added `-sign-key` CLI flag. `shiguang-launcher/internal/patching/chunk_patcher.go`: verifies signature after download. New test: `TestManifestHMACSignAndVerify` — correct key, wrong key, tampered hash, unsigned, empty key.
  - **Hub stats API + agent key rotation** — `shared/tenant/repo.go`: `ListDailyStats()` (last N days, max 90), `RotateAgentKey()` (gen_random_bytes in DB, immediate invalidation). `shiguang-hub/handlers/tenant.go`: `GET /me/stats?days=7`, `POST /me/agents/:id/rotate-key`. `shiguang-hub/internal/grpcserver/server.go`: `handleMetrics()` now stores peak_online into daily_stats instead of just logging. SPA: Dashboard shows 7-day activity chart (pure CSS bars), Agents page has rotate-key button with confirmation + one-time key display.
- **Result**: All 6 Go modules build clean. 68 tests passing (8 chunker tests). 85KB gzipped SPA. Zero regressions.

### Production Readiness Pass (COMPLETE 2026-04-12) / 生产就绪加固（完成）
- **Need**: Structured observability, operator self-service, and agent operational intelligence
- **Method**: 3 cross-cutting improvements — structured logging, Hub security endpoints, agent config awareness
- **Changes**:
  - **Structured logging with slog** — `shared/sglog/sglog.go` (new: 75 lines): Initializes `log/slog` with JSON (production) or text (dev) handler. Bridges stdlib `log.Printf` → slog via `slog.SetLogLoggerLevel()`. All binaries accept `-dev` flag. Refactored: Hub main.go (13 log calls → slog), Agent main.go (11 calls), hubconn/client.go (2 calls), grpcserver/server.go (10 calls). Existing `log.Printf` in internal packages auto-bridged through slog — no code changes needed.
  - **Hub security: password change + suspended login block** — `shared/tenant/repo.go`: New `UpdateTenantPassword()`. `shiguang-hub/handlers/tenant.go`: New `PUT /me/password` endpoint (verifies old password, bcrypt new, min 8 chars). Login handler now blocks suspended tenants with 403. SPA: New Settings page (`admin-spa/src/pages/Settings.jsx`) with password change form, account info, and sign-out. App.jsx: Added Settings sidebar link + route.
  - **Agent config hot-reload via Hub push** — `shiguang-agent/internal/hubconn/client.go`: New `FetchConfig()` method calls Hub's unary FetchConfig RPC. New `ConfigUpdateHandler` type. Agent main.go: ConfigUpdate command now triggers async FetchConfig → logs new config (tenant, servers, patch_url). Full dynamic route reload deferred (requires relay restart); operator gets immediate visibility that config was received.
- **Result**: All 6 Go modules build clean. 68 tests passing. 81KB gzipped SPA (7 pages). Zero regressions.

### Operational Readiness Pass (COMPLETE 2026-04-12) / 运维就绪加固（完成）
- **Need**: Close 4 operational gaps found in production readiness audit
- **Method**: Background cleanup, rich health checks, idempotent schema migration
- **Changes**:
  - **Token cleanup goroutine** — `shiguang-hub/cmd/hub/main.go`: Background goroutine every 10 minutes calls `repo.PurgeExpiredTokens()` — previously defined but never called. Session tokens no longer accumulate indefinitely.
  - **Hub rich health check** — `shiguang-hub/cmd/hub/main.go`: `/healthz` now pings DB (2s timeout), reports gRPC agent count, returns structured JSON `{status, database, grpc_agents}`. Returns 503 if DB unreachable.
  - **Gate rich health check** — `shiguang-gate/internal/api/admin.go`: `/healthz` now returns JSON `{status, routes, active_conn, ban_count}`. Returns 503 if all upstream routes are unhealthy. Updated test to match new JSON response.
  - **Schema auto-migration** — `shared/tenant/migrate.go` (new: 120 lines): `AutoMigrate()` function with idempotent DDL (`CREATE TABLE IF NOT EXISTS` for all 8 tables + indexes). Hub main.go calls it after DB connect, before starting services. No external migration tool needed — new Hub deployments auto-create the schema on first startup.
- **Result**: All 6 Go modules build clean. 68 tests passing. Zero regressions.

## Remaining Work / 剩余工作

- **P2**: Extract BanListInterface + TenantRepo interface for testability
- **Integration**: version.dll send() hook — CM_LOGIN packet injection (requires protocol testing with running servers)
- **Integration**: AionCore 5.8 C++ token handoff (same pattern as Beyond 4.8)
