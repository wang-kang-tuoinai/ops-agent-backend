package utils

import (
	"context"
	"errors"
	"ops-agent-backend/internal/scripts"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var unlockCmd = redis.NewScript(scripts.UnlockScript)

type RedisLocker struct {
	rdb *redis.Client
}

func NewRedisDL(rdb *redis.Client) *RedisLocker {
	return &RedisLocker{
		rdb: rdb,
	}
}

func (l *RedisLocker) TryLock(ctx context.Context, lockKey string, lockTTL time.Duration) (string, error) {
	lockValue := uuid.New().String()
	redisCreateCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	success, err := l.rdb.SetNX(redisCreateCtx, lockKey, lockValue, lockTTL).Result()
	if err != nil {
		return "", err
	}
	if !success {
		return "", ErrLockConflict
	}
	return lockValue, nil
}

func (l *RedisLocker) TryUnLock(ctx context.Context, lockKey string, lockValue string) error {
	redisCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	result, err := unlockCmd.Run(redisCtx, l.rdb, []string{lockKey}, lockValue).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return errors.New("unlock failed: lock not held by this process")
	}
	return nil
}
