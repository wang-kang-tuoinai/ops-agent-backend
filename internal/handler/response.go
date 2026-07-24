package handler

import (
	"errors"
	"log"
	"net/http"
	"ops-agent-backend/internal/repository"
	"ops-agent-backend/internal/utils"

	"github.com/gin-gonic/gin"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

func HandleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrUserNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "用户不存在"})
	case errors.Is(err, repository.ErrDuplicateUser):
		c.JSON(http.StatusConflict, ErrorResponse{Error: "用户已存在"})
	case errors.Is(err, utils.ErrLockConflict):
		c.JSON(http.StatusConflict, ErrorResponse{Error: "该用户正在被修改,请稍后再试"})
	default:
		//未预期的错误不暴露内部细节
		log.Println("internal error: ", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "服务器内部错误"})
	}
}

func BadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
}
