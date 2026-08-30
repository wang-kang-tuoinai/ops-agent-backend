package router

import (
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/observability"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func SetupRouter(h *handler.UserHandler, recorder *observability.Recorder) *gin.Engine {
	r := gin.Default()
	r.Use(otelgin.Middleware("ops-agent-backend"))
	r.Use(observability.AccessLogMiddleware(recorder))
	api := r.Group("/api/v1")
	{
		api.POST("/users", h.CreateUser)
		api.GET("/users/:id", h.GetUser)
		api.GET("/users", h.ListUser)
		api.PUT("/users/:id", h.UpdateUser)
		api.DELETE("/users/:id", h.DeleteUser)
	}
	return r
}
