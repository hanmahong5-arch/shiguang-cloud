// Package metrics 的单元测试。
// 覆盖 Handler 输出格式完整性 与 CountMiddleware 的状态码分桶逻辑。
package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// resetAll 在每个子测试前把所有计数器归零，避免顺序依赖。
// 由于 all 是包级变量且 val 指向真实的 atomic.Int64，Store(0) 即可。
func resetAll() {
	for _, m := range all {
		m.val.Store(0)
	}
}

// TestHandler_ContainsAllCounters 验证 /metrics 输出包含所有 13 个计数器
// 的 HELP/TYPE/value 三行，并且 Content-Type 为 Prometheus text 格式。
func TestHandler_ContainsAllCounters(t *testing.T) {
	resetAll()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/metrics", Handler())

	resp, err := app.Test(httptest.NewRequest("GET", "/metrics", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type=%q, want text/plain*", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	out := string(body)

	// 覆盖率断言：13 个 counter 全部现身（HELP + TYPE + value）
	if len(all) != 13 {
		t.Fatalf("expected 13 counters, got %d", len(all))
	}
	for _, m := range all {
		if !strings.Contains(out, "# HELP "+m.name+" ") {
			t.Errorf("missing HELP for %s", m.name)
		}
		if !strings.Contains(out, "# TYPE "+m.name+" counter") {
			t.Errorf("missing TYPE for %s", m.name)
		}
		if !strings.Contains(out, m.name+" 0\n") {
			t.Errorf("missing initial value 0 for %s", m.name)
		}
	}
}

// TestHandler_ReflectsCounterChanges 在计数器被修改后重新抓取 /metrics，
// 验证数值正确反映。
func TestHandler_ReflectsCounterChanges(t *testing.T) {
	resetAll()
	TokensIssued.Add(7)
	LoginsFailed.Add(3)

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/metrics", Handler())
	resp, err := app.Test(httptest.NewRequest("GET", "/metrics", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	out := string(body)

	if !strings.Contains(out, "sgcontrol_tokens_issued_total 7\n") {
		t.Errorf("tokens_issued not reflected:\n%s", out)
	}
	if !strings.Contains(out, "sgcontrol_logins_failed_total 3\n") {
		t.Errorf("logins_failed not reflected")
	}
}

// TestCountMiddleware_Buckets 表驱动覆盖状态码分桶（2xx/3xx/4xx/5xx）。
func TestCountMiddleware_Buckets(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		want4xxInc int64
		want5xxInc int64
	}{
		{"200 increments only total", 200, 0, 0},
		{"301 increments only total", 301, 0, 0},
		{"400 increments 4xx", 400, 1, 0},
		{"404 increments 4xx", 404, 1, 0},
		{"499 increments 4xx (upper edge)", 499, 1, 0},
		{"500 increments 5xx", 500, 0, 1},
		{"503 increments 5xx", 503, 0, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetAll()
			status := tc.status

			app := fiber.New(fiber.Config{DisableStartupMessage: true})
			app.Use(CountMiddleware())
			app.Get("/x", func(c *fiber.Ctx) error {
				return c.Status(status).SendString("ok")
			})

			resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
			if err != nil {
				t.Fatalf("Test: %v", err)
			}
			resp.Body.Close()

			if got := RequestsTotal.Load(); got != 1 {
				t.Errorf("RequestsTotal=%d, want 1", got)
			}
			if got := Requests4xx.Load(); got != tc.want4xxInc {
				t.Errorf("Requests4xx=%d, want %d", got, tc.want4xxInc)
			}
			if got := Requests5xx.Load(); got != tc.want5xxInc {
				t.Errorf("Requests5xx=%d, want %d", got, tc.want5xxInc)
			}
		})
	}
}

// TestCountMiddleware_MultipleRequests 多次调用累加正确。
func TestCountMiddleware_MultipleRequests(t *testing.T) {
	resetAll()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(CountMiddleware())
	app.Get("/ok", func(c *fiber.Ctx) error { return c.SendString("x") })
	app.Get("/bad", func(c *fiber.Ctx) error { return c.Status(400).SendString("x") })
	app.Get("/err", func(c *fiber.Ctx) error { return c.Status(500).SendString("x") })

	for _, path := range []string{"/ok", "/ok", "/bad", "/bad", "/err"} {
		r, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("Test %s: %v", path, err)
		}
		r.Body.Close()
	}

	if got := RequestsTotal.Load(); got != 5 {
		t.Errorf("RequestsTotal=%d, want 5", got)
	}
	if got := Requests4xx.Load(); got != 2 {
		t.Errorf("Requests4xx=%d, want 2", got)
	}
	if got := Requests5xx.Load(); got != 1 {
		t.Errorf("Requests5xx=%d, want 1", got)
	}
}
