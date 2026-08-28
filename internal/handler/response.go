package handler

import (
	"errors"
	"log"
	"net/http"
	obs "ops-agent-backend/internal/observability"
	"ops-agent-backend/internal/repository"
	"ops-agent-backend/internal/utils"

	"github.com/gin-gonic/gin"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func (h *UserHandler) HandleError(c *gin.Context, err error, attrs map[string]any) {
	ctx := c.Request.Context()
	route := c.FullPath()
	switch {
	case errors.Is(err, repository.ErrUserNotFound):
		h.recorder.Record(ctx, obs.LevelDebug, obs.TplUserNotFound, obs.WithRoute(route), obs.WithAttrs(attrs))
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "用户不存在"})
	case errors.Is(err, repository.ErrDuplicateUser):
		h.recorder.Record(ctx, obs.LevelDebug, obs.TplDuplicateUser, obs.WithRoute(route), obs.WithAttrs(attrs))
		c.JSON(http.StatusConflict, ErrorResponse{Error: "用户已存在"})
	case errors.Is(err, utils.ErrLockConflict):
		h.recorder.Record(ctx, obs.LevelDebug, obs.TplLockConflict, obs.WithRoute(route), obs.WithAttrs(attrs))
		c.JSON(http.StatusConflict, ErrorResponse{Error: "该用户正在被修改,请稍后再试"})
	default:
		//未预期的错误不暴露内部细节
		log.Println("internal error: ", err)
		if attrs == nil {
			attrs = make(map[string]any)
		}
		attrs["err"] = err.Error()
		h.recorder.Record(ctx, obs.LevelError, obs.TplInternalError, obs.WithRoute(route), obs.WithAttrs(map[string]any{}))
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "服务器内部错误"})
	}
}

func (h *UserHandler) BadRequest(c *gin.Context, err error) {
	h.recorder.Record(c.Request.Context(), obs.LevelDebug, obs.TplBadRequest,
		obs.WithRoute(c.FullPath()),
		obs.WithAttrs(map[string]any{"err": err.Error()}),
	)
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
}
