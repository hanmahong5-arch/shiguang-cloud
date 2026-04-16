// Package httputil provides RFC 7807 Problem Details for HTTP APIs.
//
// Usage:
//
//	return httputil.Problem(c, httputil.ProblemOpts{
//	    Status: 403,
//	    Title:  "Forbidden",
//	    Detail: "token validation restricted to loopback",
//	    Type:   "https://shiguang.dev/errors/loopback-only",
//	})
//
// The response Content-Type is application/problem+json (RFC 7807 §3).
// Fields absent from ProblemOpts are omitted from the JSON output.
package httputil

import (
	"github.com/gofiber/fiber/v2"
)

// ProblemOpts carries the fields for a Problem Details response.
// Only non-zero / non-empty fields are serialized.
type ProblemOpts struct {
	// HTTP 状态码（必填）
	Status int `json:"status"`
	// 简短人类可读标题，按 HTTP 状态码可重复
	Title string `json:"title"`
	// 详情（本次请求的具体原因）
	Detail string `json:"detail,omitempty"`
	// URI 引用，指向问题类型文档；省略时使用 "about:blank"
	Type string `json:"type,omitempty"`
	// 问题发生的 URI（可选）
	Instance string `json:"instance,omitempty"`
}

// Problem 写入一个 RFC 7807 application/problem+json 响应。
// 若 Status 为零则默认 500。
func Problem(c *fiber.Ctx, o ProblemOpts) error {
	if o.Status == 0 {
		o.Status = fiber.StatusInternalServerError
	}
	if o.Type == "" {
		o.Type = "about:blank"
	}
	c.Set("Content-Type", "application/problem+json")
	return c.Status(o.Status).JSON(o)
}

// NotFound 返回 404 Problem。
func NotFound(c *fiber.Ctx, detail string) error {
	return Problem(c, ProblemOpts{
		Status: fiber.StatusNotFound,
		Title:  "Not Found",
		Detail: detail,
	})
}

// BadRequest 返回 400 Problem。
func BadRequest(c *fiber.Ctx, detail string) error {
	return Problem(c, ProblemOpts{
		Status: fiber.StatusBadRequest,
		Title:  "Bad Request",
		Detail: detail,
	})
}

// Forbidden 返回 403 Problem。
func Forbidden(c *fiber.Ctx, detail string) error {
	return Problem(c, ProblemOpts{
		Status: fiber.StatusForbidden,
		Title:  "Forbidden",
		Detail: detail,
	})
}

// Unauthorized 返回 401 Problem。
func Unauthorized(c *fiber.Ctx, detail string) error {
	return Problem(c, ProblemOpts{
		Status: fiber.StatusUnauthorized,
		Title:  "Unauthorized",
		Detail: detail,
	})
}
