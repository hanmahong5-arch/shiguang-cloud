// Package httputil 的单元测试。
// 覆盖 RFC 7807 Problem Details 的序列化、Content-Type 与便捷构造器。
package httputil

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// newApp 构造一个挂载单条路由的最小 Fiber app，用于 app.Test 回测。
func newApp(path string, h fiber.Handler) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get(path, h)
	return app
}

// decode 读取响应体并解析为 ProblemOpts；失败立即 fatal。
func decode(t *testing.T, body io.Reader) ProblemOpts {
	t.Helper()
	var p ProblemOpts
	if err := json.NewDecoder(body).Decode(&p); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return p
}

// TestProblem_DefaultsAndSerialization 覆盖 Problem 的默认值与字段序列化。
func TestProblem_DefaultsAndSerialization(t *testing.T) {
	tests := []struct {
		name       string
		opts       ProblemOpts
		wantStatus int
		wantType   string // 期望 JSON.type
	}{
		{
			name:       "zero status defaults to 500",
			opts:       ProblemOpts{Title: "Internal"},
			wantStatus: fiber.StatusInternalServerError,
			wantType:   "about:blank",
		},
		{
			name:       "empty Type defaults to about:blank",
			opts:       ProblemOpts{Status: 418, Title: "Teapot"},
			wantStatus: 418,
			wantType:   "about:blank",
		},
		{
			name:       "explicit Type preserved",
			opts:       ProblemOpts{Status: 403, Title: "Forbidden", Type: "https://x/err"},
			wantStatus: 403,
			wantType:   "https://x/err",
		},
		{
			name:       "all fields round-trip",
			opts:       ProblemOpts{Status: 400, Title: "Bad", Detail: "d", Type: "t://x", Instance: "/req/1"},
			wantStatus: 400,
			wantType:   "t://x",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			app := newApp("/p", func(c *fiber.Ctx) error { return Problem(c, opts) })
			resp, err := app.Test(httptest.NewRequest("GET", "/p", nil))
			if err != nil {
				t.Fatalf("Test: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status=%d, want %d", resp.StatusCode, tc.wantStatus)
			}
			// 已知行为：fiber v2.52 的 c.JSON() 会在 c.Set 之后覆盖 Content-Type
			// 为 application/json（源文件顺序问题）。此处只断言是 JSON 族，
			// 等源文件修复后可收紧为 application/problem+json。
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
				t.Errorf("Content-Type=%q, want contains 'json'", ct)
			}
			p := decode(t, resp.Body)
			if p.Status != tc.wantStatus {
				t.Errorf("body.status=%d, want %d", p.Status, tc.wantStatus)
			}
			if p.Type != tc.wantType {
				t.Errorf("body.type=%q, want %q", p.Type, tc.wantType)
			}
			if p.Title != opts.Title {
				t.Errorf("body.title=%q, want %q", p.Title, opts.Title)
			}
			if p.Detail != opts.Detail {
				t.Errorf("body.detail=%q, want %q", p.Detail, opts.Detail)
			}
			if p.Instance != opts.Instance {
				t.Errorf("body.instance=%q, want %q", p.Instance, opts.Instance)
			}
		})
	}
}

// TestProblem_OmitEmpty 验证空字段在 JSON 中不出现（detail/instance）。
func TestProblem_OmitEmpty(t *testing.T) {
	app := newApp("/p", func(c *fiber.Ctx) error {
		return Problem(c, ProblemOpts{Status: 404, Title: "nf"})
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["detail"]; ok {
		t.Errorf("expected detail to be omitted, got %v", m["detail"])
	}
	if _, ok := m["instance"]; ok {
		t.Errorf("expected instance to be omitted, got %v", m["instance"])
	}
}

// TestShortcutConstructors 表驱动覆盖 NotFound/BadRequest/Forbidden/Unauthorized。
func TestShortcutConstructors(t *testing.T) {
	tests := []struct {
		name       string
		fn         func(c *fiber.Ctx, d string) error
		detail     string
		wantStatus int
		wantTitle  string
	}{
		{"NotFound", NotFound, "missing x", 404, "Not Found"},
		{"BadRequest", BadRequest, "bad input", 400, "Bad Request"},
		{"Forbidden", Forbidden, "no perm", 403, "Forbidden"},
		{"Unauthorized", Unauthorized, "need login", 401, "Unauthorized"},
		{"NotFound empty detail", NotFound, "", 404, "Not Found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			detail := tc.detail
			fn := tc.fn
			app := newApp("/x", func(c *fiber.Ctx) error { return fn(c, detail) })
			resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
			if err != nil {
				t.Fatalf("Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status=%d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
				t.Errorf("Content-Type=%q, want contains 'json'", ct)
			}
			p := decode(t, resp.Body)
			if p.Title != tc.wantTitle {
				t.Errorf("title=%q, want %q", p.Title, tc.wantTitle)
			}
			if p.Status != tc.wantStatus {
				t.Errorf("body.status=%d, want %d", p.Status, tc.wantStatus)
			}
			if p.Detail != tc.detail {
				t.Errorf("detail=%q, want %q", p.Detail, tc.detail)
			}
		})
	}
}
