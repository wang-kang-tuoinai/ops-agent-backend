package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"ops-agent-backend/internal/model"
	obs "ops-agent-backend/internal/observability"
	"ops-agent-backend/internal/repository"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("cache")

type CacheUserRepository struct {
	redis    *redis.Client
	next     repository.UserRepository
	recorder *obs.Recorder
}

func NewUserCacheRepository(next repository.UserRepository, rdb *redis.Client, recorder *obs.Recorder) *CacheUserRepository {
	return &CacheUserRepository{
		redis:    rdb,
		next:     next,
		recorder: recorder,
	}
}

func (c *CacheUserRepository) Create(ctx context.Context, u model.User) (model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.Create")
	defer span.End()
	newUser, err := c.next.Create(ctx, u)
	if err != nil {
		return model.User{}, err
	}
	span.SetAttributes(attribute.Int64("user.id", newUser.ID))
	//把model.User转化为CachedUser再存储
	cachedUser := ToCachedUser(newUser)
	jsonData, err := json.Marshal(cachedUser)
	storeOK := true
	if err != nil {
		span.RecordError(err)
		storeOK = false
		log.Println("Marshal user for cache failed:", err)
		span.SetAttributes(attribute.Bool("cache.store", storeOK))
		return newUser, nil
	}

	key := fmt.Sprintf("user:%d", newUser.ID)
	redisCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	err = c.redis.Set(redisCtx, key, jsonData, 10*time.Minute).Err()
	if err != nil {
		span.RecordError(err)
		storeOK = false
		log.Println("Cache user failed:", err)
		c.recorder.Record(ctx, obs.LevelWarn, obs.TplCacheStoreFailed,
			obs.WithAttrs(map[string]any{"user_id": newUser.ID, "err": err.Error(), "component": "redis"}))
	}
	span.SetAttributes(attribute.Bool("cache.store", storeOK))
	return newUser, nil
}

// GetByID返回用户资料,当缓存命中的时候,User结构体里的password字段为空
// 若需要密码(见TODO: GetByUsernameForAuth)
func (c *CacheUserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.GetById")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	key := fmt.Sprintf("user:%d", id)
	redisGetCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	jsonData, err := c.redis.Get(redisGetCtx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		span.SetAttributes(attribute.Bool("cache.hit", false))
		c.recorder.Record(ctx, obs.LevelDebug, obs.TplCacheMiss,
			obs.WithAttrs(map[string]any{"user_id": id, "component": "redis"}))
		// Redis里数据不存在,从repository层获取数据
		u, err := c.next.GetById(ctx, id)
		if err != nil {
			return model.User{}, err
		}
		//把model.User转化为CachedUser存储在Redis中,隐藏密码
		cachedUser := ToCachedUser(u)
		// 尝试存入Redis
		data, err := json.Marshal(cachedUser)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("cache.store", false))
			log.Println("Marshal user for cache failed:", err)
			return u, nil
		}
		redisSetCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
		defer cancel()
		err = c.redis.Set(redisSetCtx, key, data, 10*time.Minute).Err()
		storeOK := true
		if err != nil {
			storeOK = false
			log.Println("Cache user failed:", err)
			c.recorder.Record(ctx, obs.LevelWarn, obs.TplCacheStoreFailed,
				obs.WithAttrs(map[string]any{"user_id": id, "err": err.Error(), "component": "redis"}))
		}
		span.SetAttributes(attribute.Bool("cache.store", storeOK))
		return u, nil
	} else if err != nil {
		// 如果从Redis获取数据发生错误,那么直接从Repository中获取并记录日志
		log.Println("Get user from redis failed:", err)
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("cache.hit", false))
		c.recorder.Record(ctx, obs.LevelWarn, obs.TplCacheReadFailed,
			obs.WithAttrs(map[string]any{"user_id": id, "err": err.Error(), "component": "redis"}))
		u, err := c.next.GetById(ctx, id)
		if err != nil {
			return model.User{}, err
		}
		return u, nil
	} else {
		span.SetAttributes(attribute.Bool("cache.hit", true))
		// 成功从Redis中获取到数据,直接返回
		var cu CachedUser
		if err := json.Unmarshal(jsonData, &cu); err != nil {
			span.RecordError(err)
			return model.User{}, fmt.Errorf("cache: unmarshal cached user: %w", err)
		}
		return cu.ToModelUser(), nil
	}
}

func (c *CacheUserRepository) List(ctx context.Context, offset, limit int) ([]model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.List")
	defer span.End()
	span.SetAttributes(attribute.Int("offset", offset), attribute.Int("limit", limit))
	userList, err := c.next.List(ctx, offset, limit)
	if err != nil {
		return make([]model.User, 0), err
	}
	return userList, nil
}

func (c *CacheUserRepository) ListAllIDs(ctx context.Context) ([]int64, error) {
	userIDs, err := c.next.ListAllIDs(ctx)
	if err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (c *CacheUserRepository) Update(ctx context.Context, id int64, upd model.UserUpdate) (model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.Update")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	newUser, err := c.next.Update(ctx, id, upd)
	if err != nil {
		return model.User{}, err
	}
	//成功从数据库里更新数据之后,把Redis里的旧数据删掉
	key := fmt.Sprintf("user:%d", id)
	redisDelCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	_, err = c.redis.Del(redisDelCtx, key).Result()
	delOK := true
	if err != nil {
		span.RecordError(err)
		delOK = false
		log.Println("Del user in redis:", err)
		c.recorder.Record(ctx, obs.LevelWarn, obs.TplCacheDelFailed,
			obs.WithAttrs(map[string]any{"user_id": id, "err": err.Error(), "component": "redis"}))
	}
	span.SetAttributes(attribute.Bool("cache.del", delOK))
	return newUser, nil
}

func (c *CacheUserRepository) Delete(ctx context.Context, id int64) error {
	ctx, span := tracer.Start(ctx, "cache.Delete")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	if err := c.next.Delete(ctx, id); err != nil {
		return err
	}
	// 删除Redis里的数据
	key := fmt.Sprintf("user:%d", id)
	redisDelCtx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	_, err := c.redis.Del(redisDelCtx, key).Result()
	delOK := true
	if err != nil {
		span.RecordError(err)
		delOK = false
		log.Println("Del user in redis failed:", err)
		c.recorder.Record(ctx, obs.LevelWarn, obs.TplCacheDelFailed,
			obs.WithAttrs(map[string]any{"user_id": id, "err": err.Error(), "component": "redis"}))
	}
	span.SetAttributes(attribute.Bool("cache.del", delOK))
	return nil
}
