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
)

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
	newUser, err := c.next.Create(ctx, u)
	if err != nil {
		return model.User{}, err
	}
	jsonData, err := json.Marshal(newUser)
	if err != nil {
		log.Println("Marshal user for cache failed:", err)
		return newUser, nil
	}

	key := fmt.Sprintf("user:%d", newUser.ID)
	redisCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	err = c.redis.Set(redisCtx, key, jsonData, 10*time.Minute).Err()
	if err != nil {
		log.Println("Cache user failed:", err)
	}

	return newUser, nil
}

func (c *CacheUserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	key := fmt.Sprintf("user:%d", id)
	redisGetCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	jsonData, err := c.redis.Get(redisGetCtx, key).Bytes()
	if err == redis.Nil {
		// Redis里数据不存在,从repository层获取数据
		u, err := c.next.GetById(ctx, id)
		// 尝试存入Redis
		if err != nil {
			return model.User{}, err
		}
		data, err := json.Marshal(u)
		if err != nil {
			log.Println("Marshal user for cache failed:", err)
			return u, nil
		}
		redisSetCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		err = c.redis.Set(redisSetCtx, key, data, 10*time.Minute).Err()
		if err != nil {
			log.Println("Cache user failed:", err)
		}

		return u, nil
	} else if err != nil {
		// 如果从Redis获取数据发生错误,那么直接从Repository中获取并记录日志
		log.Println("Get user from redis failed:", err)
		u, err := c.next.GetById(ctx, id)
		if err != nil {
			return model.User{}, err
		}
		return u, nil
	} else {
		// 成功从Redis中获取到数据,直接返回
		var u model.User
		if err := json.Unmarshal(jsonData, &u); err != nil {
			return model.User{}, err
		}
		return u, nil
	}
}

func (c *CacheUserRepository) GetAll(ctx context.Context) ([]model.User, error) {
	userList, err := c.next.GetAll(ctx)
	if err != nil {
		return make([]model.User, 0), err
	}
	return userList, nil
}

func (c *CacheUserRepository) Update(ctx context.Context, id int64, u model.User) (model.User, error) {
	newUser, err := c.next.Update(ctx, id, u)
	if err != nil {
		return model.User{}, err
	}
	//成功从数据库里更新数据之后,把Redis里的旧数据删掉
	key := fmt.Sprintf("user:%d", id)
	redisDelCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err = c.redis.Del(redisDelCtx, key).Result()
	if err != nil {
		log.Println("Del user in redis:", err)
	}
	return newUser, nil
}

func (c *CacheUserRepository) Delete(ctx context.Context, id int64) error {
	if err := c.next.Delete(ctx, id); err != nil {
		return err
	}
	// 删除Redis里的数据
	key := fmt.Sprintf("user:%d", id)
	redisDelCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err := c.redis.Del(redisDelCtx, key).Result()
	if err != nil {
		log.Println("Del user in redis failed:", err)
	}
	return nil
}
