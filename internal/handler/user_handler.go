package handler

import (
	"net/http"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"strconv"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userRepository *repository.UserRepository
}

func NewUserHandler(repo *repository.UserRepository) *UserHandler {
	return &UserHandler{userRepository: repo}
}
func (h *UserHandler) CreateUser(c *gin.Context) {
	var u model.User
	if err := c.ShouldBindJSON(&u); err != nil {
		BadRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, h.userRepository.Create(u))
}

func (h *UserHandler) GetUser(c *gin.Context) {
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	u, err := h.userRepository.GetById(id)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (h *UserHandler) ListUser(c *gin.Context) {
	users := h.userRepository.GetAll()
	c.JSON(http.StatusOK, users)
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	var u model.User
	if err := c.ShouldBindJSON(&u); err != nil {
		BadRequest(c, err)
		return
	}
	updated, err := h.userRepository.Update(id, u)
	if err != nil {
		HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		BadRequest(c, err)
		return
	}
	if err := h.userRepository.Delete(id); err != nil {
		HandleError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
