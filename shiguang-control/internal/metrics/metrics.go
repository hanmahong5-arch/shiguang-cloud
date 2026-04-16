// Package metrics provides lightweight Prometheus-format counters for
// shiguang-control. Zero external dependencies — uses sync/atomic and a
// simple text/plain serializer conforming to the Prometheus exposition
// format (https://prometheus.io/docs/instrumenting/exposition_formats/).
//
// Design rationale:
//   - Avoid pulling in prometheus/client_golang for a handful of counters
//   - Counters are monotonically increasing — no gauges/histograms for now
//   - Thread-safe: only atomic ops, no Mutex
//   - Namespace all metrics under "sgcontrol_" to avoid collisions
package metrics

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/gofiber/fiber/v2"
)

// ── Counter 定义 ─────────────────────────────────────────────────────

// 认证链路
var (
	TokensIssued   atomic.Int64 // control 签发 session token
	TokensConsumed atomic.Int64 // token 被 LS 成功消费
	TokensRejected atomic.Int64 // token 验证失败（过期/不存在/重放）
	TokensBlocked  atomic.Int64 // 被 loopback 白名单拦截

	LoginsOK     atomic.Int64 // 账号密码登录成功
	LoginsFailed atomic.Int64 // 登录失败（密码错/账号不存在）

	RegistersOK     atomic.Int64 // 注册成功
	RegistersFailed atomic.Int64 // 注册失败

	ExternalAuthOK     atomic.Int64 // ExternalAuth 成功
	ExternalAuthFailed atomic.Int64 // ExternalAuth 失败
)

// 请求粒度
var (
	RequestsTotal atomic.Int64 // 所有 HTTP 请求计数
	Requests4xx   atomic.Int64 // 4xx 响应
	Requests5xx   atomic.Int64 // 5xx 响应
)

// counter 是一个 (name, help, *atomic.Int64) 三元组
type counter struct {
	name string
	help string
	val  *atomic.Int64
}

// all 声明全部导出指标（顺序即 /metrics 输出顺序）
var all = []counter{
	{"sgcontrol_tokens_issued_total", "Total session tokens issued via /api/account/login", &TokensIssued},
	{"sgcontrol_tokens_consumed_total", "Tokens consumed via /api/token/validate (success)", &TokensConsumed},
	{"sgcontrol_tokens_rejected_total", "Tokens rejected (expired/unknown/replayed)", &TokensRejected},
	{"sgcontrol_tokens_blocked_total", "Token validate calls blocked by loopback whitelist", &TokensBlocked},

	{"sgcontrol_logins_ok_total", "Successful password logins", &LoginsOK},
	{"sgcontrol_logins_failed_total", "Failed password logins", &LoginsFailed},

	{"sgcontrol_registers_ok_total", "Successful account registrations", &RegistersOK},
	{"sgcontrol_registers_failed_total", "Failed account registrations", &RegistersFailed},

	{"sgcontrol_external_auth_ok_total", "ExternalAuth success", &ExternalAuthOK},
	{"sgcontrol_external_auth_failed_total", "ExternalAuth failure", &ExternalAuthFailed},

	{"sgcontrol_requests_total", "Total HTTP requests", &RequestsTotal},
	{"sgcontrol_requests_4xx_total", "HTTP 4xx responses", &Requests4xx},
	{"sgcontrol_requests_5xx_total", "HTTP 5xx responses", &Requests5xx},
}

// Handler 返回 Fiber handler，以 text/plain Prometheus 格式导出所有计数器。
// 仅供 /metrics 端点注册使用。
func Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		for _, m := range all {
			fmt.Fprintf(&b, "# HELP %s %s\n", m.name, m.help)
			fmt.Fprintf(&b, "# TYPE %s counter\n", m.name)
			fmt.Fprintf(&b, "%s %d\n", m.name, m.val.Load())
		}
		return c.SendString(b.String())
	}
}

// CountMiddleware 是 Fiber 中间件：每请求 RequestsTotal++，
// 响应完成后按状态码分桶 4xx/5xx。
func CountMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		RequestsTotal.Add(1)
		err := c.Next()
		status := c.Response().StatusCode()
		if status >= 400 && status < 500 {
			Requests4xx.Add(1)
		} else if status >= 500 {
			Requests5xx.Add(1)
		}
		return err
	}
}
