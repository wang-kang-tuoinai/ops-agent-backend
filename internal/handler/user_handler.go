package handler

import (
	"net/http"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"strconv"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userRepository repository.UserRepository
}

func NewUserHandler(repo repository.UserRepository) *UserHandler {
	return &UserHandler{userRepository: repo}
}
func (h *UserHandler) CreateUser(c *gin.Context) {
	ctx := c.Request.Context()
	var req model.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err)
		return
	}
	var u model.User
	if req.Age != nil {
		u.Age = *req.Age
	}
	u.Email = req.Email
	u.Password = req.Password
	u.Username = req.Username
	newUser, err := h.userRepository.Create(ctx, u)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, newUser)
}

func (h *UserHandler) GetUser(c *gin.Context) {
	ctx := c.Request.Context()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	u, err := h.userRepository.GetById(ctx, id)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (h *UserHandler) ListUser(c *gin.Context) {
	ctx := c.Request.Context()
	users, err := h.userRepository.GetAll(ctx)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, users)
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	var req model.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err)
		return
	}
	//TODO 这里会有并发请求冲突 后需要加锁
	// 查旧数据
	existing, err := h.userRepository.GetById(ctx, id)
	if err != nil {
		HandleError(c, err)
		return
	}
	if req.Age != nil {
		existing.Age = *req.Age
	}
	if req.Email != nil {
		existing.Email = *req.Email
	}
	if req.Username != nil {
		existing.Username = *req.Username
	}

	updated, err := h.userRepository.Update(ctx, id, existing)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	if err := h.userRepository.Delete(ctx, id); err != nil {
		HandleError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
