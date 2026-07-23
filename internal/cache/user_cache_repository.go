package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("cache")

type CacheUserRepository struct {
	redis *redis.Client
	next  repository.UserRepository
}

func NewUserCacheRepository(next repository.UserRepository, rdb *redis.Client) *CacheUserRepository {
	return &CacheUserRepository{
		redis: rdb,
		next:  next,
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
	jsonData, err := json.Marshal(newUser)
	storeOK := true
	if err != nil {
		span.RecordError(err)
		storeOK = false
		log.Println("Marshal user for cache failed:", err)
		return newUser, nil
	}

	key := fmt.Sprintf("user:%d", newUser.ID)
	redisCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	err = c.redis.Set(redisCtx, key, jsonData, 10*time.Minute).Err()
	if err != nil {
		span.RecordError(err)
		storeOK = false
		log.Println("Cache user failed:", err)
	}
	span.SetAttributes(attribute.Bool("cache.store", storeOK))
	return newUser, nil
}

func (c *CacheUserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.GetById")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	key := fmt.Sprintf("user:%d", id)
	redisGetCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	jsonData, err := c.redis.Get(redisGetCtx, key).Bytes()
	if err == redis.Nil {
		span.SetAttributes(attribute.Bool("cache.hit", false))
		// Redis里数据不存在,从repository层获取数据
		u, err := c.next.GetById(ctx, id)
		if err != nil {
			return model.User{}, err
		}
		// 尝试存入Redis
		data, err := json.Marshal(u)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("cache.store", false))
			log.Println("Marshal user for cache failed:", err)
			return u, nil
		}
		redisSetCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		err = c.redis.Set(redisSetCtx, key, data, 10*time.Minute).Err()
		if err != nil {
			span.SetAttributes(attribute.Bool("cache.store", false))
			log.Println("Cache user failed:", err)
		}
		span.SetAttributes(attribute.Bool("cache.store", true))
		return u, nil
	} else if err != nil {
		// 如果从Redis获取数据发生错误,那么直接从Repository中获取并记录日志
		log.Println("Get user from redis failed:", err)
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("cache.hit", false))
		span.SetAttributes(attribute.Bool("cache.store", false))
		u, err := c.next.GetById(ctx, id)
		if err != nil {
			return model.User{}, err
		}
		return u, nil
	} else {
		span.SetAttributes(attribute.Bool("cache.hit", true))
		// 成功从Redis中获取到数据,直接返回
		var u model.User
		if err := json.Unmarshal(jsonData, &u); err != nil {
			span.RecordError(err)
			return model.User{}, err
		}
		return u, nil
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

func (c *CacheUserRepository) Update(ctx context.Context, id int64, u model.User) (model.User, error) {
	ctx, span := tracer.Start(ctx, "cache.Update")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	newUser, err := c.next.Update(ctx, id, u)
	if err != nil {
		return model.User{}, err
	}
	//成功从数据库里更新数据之后,把Redis里的旧数据删掉
	key := fmt.Sprintf("user:%d", id)
	redisDelCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err = c.redis.Del(redisDelCtx, key).Result()
	delOK := true
	if err != nil {
		span.RecordError(err)
		delOK = false
		log.Println("Del user in redis:", err)
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
	redisDelCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err := c.redis.Del(redisDelCtx, key).Result()
	delOK := true
	if err != nil {
		span.RecordError(err)
		delOK = false
		log.Println("Del user in redis failed:", err)
	}
	span.SetAttributes(attribute.Bool("cache.del", delOK))
	return nil
}
