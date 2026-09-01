package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// 私有 context key 类型，避免与其他包的 key 碰撞
type ctxKeyRoute struct{}
type ctxKeyMethod struct{}

const (
	unmatchedRoute = "<unmatched>"
)

// RouteFromContext 从 ctx 中读取路由模板，未设置时返回空字符串。
func RouteFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRoute{}).(string); ok {
		return v
	}
	return ""
}

// MethodFromContext 从 ctx 中读取 HTTP 方法，未设置时返回空字符串。
func MethodFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyMethod{}).(string); ok {
		return v
	}
	return ""
}

// AccessLogMiddleware 返回一个 Gin 中间件，将每条 HTTP 请求记录为 access log。
// 日志级别根据响应状态码决定：5xx → Error，4xx → Warn，其余 → Info。
// recorder 为 nil 时中间件会跳过记录（不 panic），方便测试。
func AccessLogMiddleware(recorder *Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 计算路由模板
		// c.FullPath() 在 404（未匹配任何路由）时返回空字符串
		fullPath := c.FullPath()
		route := fullPath
		if fullPath == "" {
			route = unmatchedRoute
		}

		// 把 route 和 method 注入 ctx，供下游 handler / cache 层直接取用
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, ctxKeyRoute{}, route)
		ctx = context.WithValue(ctx, ctxKeyMethod{}, c.Request.Method)
		c.Request = c.Request.WithContext(ctx)

		// 放行，等 handler 执行完毕后再读取 status
		c.Next()

		template := fmt.Sprintf("%s %s", c.Request.Method, fullPath)
		if fullPath == "" {
			template = unmatchedRoute
		}

		status := c.Writer.Status()
		level := LevelInfo
		if status >= 500 {
			level = LevelError
		}
		// 4xx 保持 INFO，具体的业务判断交给 HandleError

		recorder.Record(
			c.Request.Context(),
			level,
			template,
			WithRoute(route),
			WithMethod(c.Request.Method),
			WithAttrs(map[string]any{
				"status":      status,
				"duration_ms": time.Since(start).Milliseconds(),
				"path":        c.Request.URL.Path, // 实际路径，含真实 ID
			}),
		)
	}
}
