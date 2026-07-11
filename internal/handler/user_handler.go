package handler

import (
	"fmt"
	"log"
	"net/http"
	"ops-agent-backend/internal/bloom"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"ops-agent-backend/internal/utils"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userRepository repository.UserRepository
	redisLocker    *utils.RedisLocker
	bloomFilter    *bloom.BloomFilter
}

func NewUserHandler(repo repository.UserRepository, redisLocker *utils.RedisLocker, bf *bloom.BloomFilter) *UserHandler {
	return &UserHandler{userRepository: repo, redisLocker: redisLocker, bloomFilter: bf}
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
	h.bloomFilter.Add(fmt.Sprintf("%d", newUser.ID))
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
	if !h.bloomFilter.MightContain(strID) {
		log.Println("blocked by bloom filter:", strID)
		HandleError(c, repository.ErrUserNotFound)
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
	lockKey := fmt.Sprintf("lock:user:%d", id)
	lockValue, err := h.redisLocker.TryLock(ctx, lockKey, 4*time.Second)
	if err != nil {
		HandleError(c, err)
		return
	}
	defer func() {
		if err := h.redisLocker.TryUnLock(ctx, lockKey, lockValue); err != nil {
			log.Println("Try unlock redis lock failed:", err)
		}
	}()
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
