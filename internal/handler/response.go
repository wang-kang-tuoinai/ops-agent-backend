package handler

import (
	"errors"
	"log"
	"net/http"
	"ops-agent-backend/internal/apperr"
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
	switch {
	case errors.Is(err, repository.ErrUserNotFound):
		h.recorder.Record(ctx, obs.LevelInfo, obs.TplUserNotFound, obs.WithAttrs(attrs))
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "用户不存在"})
	case errors.Is(err, repository.ErrDuplicateUser):
		h.recorder.Record(ctx, obs.LevelInfo, obs.TplDuplicateUser, obs.WithAttrs(attrs))
		c.JSON(http.StatusConflict, ErrorResponse{Error: "用户已存在"})
	case errors.Is(err, utils.ErrLockConflict):
		h.recorder.Record(ctx, obs.LevelWarn, obs.TplLockConflict, obs.WithAttrs(attrs))
		c.JSON(http.StatusConflict, ErrorResponse{Error: "该用户正在被修改,请稍后再试"})
	default:
		//未预期的错误不暴露内部细节
		log.Println("internal error: ", err)
		template := obs.TplInternalError
		switch {
		case errors.Is(err, apperr.ErrMySQL):
			template = obs.TplInternalErrorMySQL
		case errors.Is(err, apperr.ErrCache):
			template = obs.TplInternalErrorCache
		case errors.Is(err, apperr.ErrMQ):
			template = obs.TplInternalErrorMQ
		}
		if attrs == nil {
			attrs = make(map[string]any)
		}
		attrs["err"] = err.Error()
		h.recorder.Record(ctx, obs.LevelError, template, obs.WithAttrs(attrs))
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "服务器内部错误"})
	}
}

func (h *UserHandler) BadRequest(c *gin.Context, err error) {
	h.recorder.Record(c.Request.Context(), obs.LevelInfo, obs.TplBadRequest,
		obs.WithAttrs(map[string]any{"err": err.Error()}),
	)
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
}
