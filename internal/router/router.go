package router

import (
	"ops-agent-backend/internal/handler"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func SetupRouter(h *handler.UserHandler) *gin.Engine {
	r := gin.Default()
	r.Use(otelgin.Middleware("ops-agent-backend"))
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
