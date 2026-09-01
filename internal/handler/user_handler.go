package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"ops-agent-backend/internal/bloom"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/mq"
	obs "ops-agent-backend/internal/observability"
	"ops-agent-backend/internal/repository"
	"ops-agent-backend/internal/utils"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/crypto/bcrypt"
)

var tracer = otel.Tracer("handler")

type UserHandler struct {
	userRepository repository.UserRepository
	redisLocker    *utils.RedisLocker
	bloomFilter    *bloom.BloomFilter
	publisher      *mq.Publisher
	recorder       *obs.Recorder
}

func NewUserHandler(repo repository.UserRepository, redisLocker *utils.RedisLocker, bf *bloom.BloomFilter, pb *mq.Publisher, recorder *obs.Recorder) *UserHandler {
	return &UserHandler{userRepository: repo, redisLocker: redisLocker, bloomFilter: bf, publisher: pb, recorder: recorder}
}
func (h *UserHandler) CreateUser(c *gin.Context) {
	ctx := c.Request.Context()
	ctx, span := tracer.Start(ctx, "handler.create.user")
	defer span.End()
	var req model.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, err)
		return
	}
	var u model.User
	if req.Age != nil {
		u.Age = *req.Age
	}
	u.Email = req.Email
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.HandleError(c, fmt.Errorf("handler: hash password: %w", err), nil)
		return
	}
	u.Password = string(hashedBytes)
	u.Username = req.Username
	newUser, err := h.userRepository.Create(ctx, u)
	if err != nil {
		h.HandleError(c, err, nil)
		return
	}
	h.bloomFilter.Add(fmt.Sprintf("%d", newUser.ID))
	// 发布用户注册事件
	event := mq.UserRegisterEvent{
		UserId:   newUser.ID,
		UserName: newUser.Username,
	}
	publishOK := true
	//TODO Trace Context没有注入AMQP header，导致consumer那边没有traceID
	if err := h.publisher.PublishUserRegister(ctx, event); err != nil {
		span.RecordError(err)
		publishOK = false
		log.Printf("用户注册事件发布失败:userId=%d err=%v\n", newUser.ID, err)
		h.recorder.Record(ctx, obs.LevelWarn, obs.TplPublishFailed, obs.WithRoute(c.FullPath()), obs.WithAttrs(map[string]any{"user_id": newUser.ID, "err": err.Error(), "component": "rabbitmq"}))
	}
	span.SetAttributes(attribute.Bool("handler.publish", publishOK))
	c.JSON(http.StatusOK, model.ToUserResponse(newUser))
}

func (h *UserHandler) GetUser(c *gin.Context) {
	ctx := c.Request.Context()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		h.BadRequest(c, err)
		return
	}
	if !h.bloomFilter.MightContain(strID) {
		log.Println("blocked by bloom filter:", strID)
		h.recorder.Record(ctx, obs.LevelInfo, obs.TplBloomBlocked, obs.WithRoute(c.FullPath()), obs.WithAttrs(map[string]any{"user_id": id}))
		h.HandleError(c, repository.ErrUserNotFound, map[string]any{"user_id": id})
		return
	}
	u, err := h.userRepository.GetById(ctx, id)
	if err != nil {
		h.HandleError(c, err, map[string]any{"user_id": id})
		return
	}
	c.JSON(http.StatusOK, model.ToUserResponse(u))
}

func (h *UserHandler) ListUser(c *gin.Context) {
	ctx := c.Request.Context()
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 20
	}
	// 限制最大limit数,防止前端传一个很大的数字拖垮数据库
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	users, err := h.userRepository.List(ctx, offset, limit)
	if err != nil {
		h.HandleError(c, err, map[string]any{"offset": offset, "limit": limit})
		return
	}
	resp := make([]model.UserResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, model.ToUserResponse(u))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *UserHandler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	ctx, span := tracer.Start(ctx, "handler.update.user")
	defer span.End()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		h.BadRequest(c, err)
		return
	}
	var req model.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, err)
		return
	}
	lockKey := fmt.Sprintf("lock:user:%d", id)
	lockValue, err := h.redisLocker.TryLock(ctx, lockKey, 4*time.Second)
	if err != nil {
		attrs := map[string]any{"user_id": id}
		if !errors.Is(err, utils.ErrLockConflict) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "try get lock failed")
			attrs["component"] = "redis"
		}
		span.SetAttributes(attribute.Bool("handler.lock", false))
		h.HandleError(c, err, attrs)
		return
	}
	span.SetAttributes(attribute.Bool("handler.lock", true))
	defer func() {
		if err := h.redisLocker.TryUnLock(ctx, lockKey, lockValue); err != nil {
			span.SetAttributes(attribute.Bool("handler.unlock", false))
			h.recorder.Record(ctx, obs.LevelWarn, obs.TplUnlockFailed, obs.WithRoute(c.FullPath()), obs.WithAttrs(map[string]any{"lock_key": lockKey, "err": err.Error(), "component": "redis"}))
			log.Println("Try unlock redis lock failed:", err)
			return
		}
		span.SetAttributes(attribute.Bool("handler.unlock", true))
	}()

	updated, err := h.userRepository.Update(ctx, id, req.ToUserUpdate())
	if err != nil {
		h.HandleError(c, err, map[string]any{"user_id": id})
		return
	}
	c.JSON(http.StatusOK, model.ToUserResponse(updated))
}

func (h *UserHandler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()
	strID := c.Param("id")
	id, err := strconv.ParseInt(strID, 10, 64)
	if err != nil {
		h.BadRequest(c, err)
		return
	}
	if !h.bloomFilter.MightContain(strID) {
		log.Println("blocked by bloom filter:", strID)
		h.recorder.Record(ctx, obs.LevelInfo, obs.TplBloomBlocked, obs.WithRoute(c.FullPath()), obs.WithAttrs(map[string]any{"user_id": id}))
		h.HandleError(c, repository.ErrUserNotFound, map[string]any{"user_id": id})
		return
	}
	if err := h.userRepository.Delete(ctx, id); err != nil {
		h.HandleError(c, err, map[string]any{"user_id": id})
		return
	}
	c.Status(http.StatusOK)
}
