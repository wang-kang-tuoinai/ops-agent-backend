package observability

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	levelInfo      = "INFO"
	unmatchedRoute = "<unmatched>"
)

// AccessLogMiddleware 返回一个 Gin 中间件，将每条 HTTP 请求记录为 INFO access log。
// recorder 为 nil 时中间件会跳过记录（不 panic），方便测试。
func AccessLogMiddleware(recorder *Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 先放行，等 handler 执行完毕后再读取 status
		c.Next()

		// 计算路由模板
		// c.FullPath() 在 404（未匹配任何路由）时返回空字符串
		fullPath := c.FullPath()
		template := fmt.Sprintf("%s %s", c.Request.Method, fullPath)
		route := fullPath
		if fullPath == "" {
			template = unmatchedRoute
			route = unmatchedRoute
		}

		recorder.Record(
			c.Request.Context(),
			levelInfo,
			template,
			WithRoute(route),
			WithAttrs(map[string]any{
				"method":      c.Request.Method,
				"status":      c.Writer.Status(),
				"duration_ms": time.Since(start).Milliseconds(),
				"path":        c.Request.URL.Path, // 实际路径，含真实 ID
			}),
		)
	}
}
