package main

import (
	"context"
	"fmt"
	"log"
	"ops-agent-backend/internal/bloom"
	"ops-agent-backend/internal/cache"
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/mq"
	"ops-agent-backend/internal/repository/memory"
	"ops-agent-backend/internal/router"
	"ops-agent-backend/internal/utils"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

func main() {
	repo := memory.NewUserMemoryRepository()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	repoCache := cache.NewUserCacheRepository(repo, rdb)
	bf := bloom.NewBloomFilter(10000, 4)
	users, err := repoCache.GetAll(context.Background())
	if err != nil {
		log.Println("Get all users failed:", err)
	}
	for _, u := range users {
		bf.Add(fmt.Sprintf("%d", u.ID))
	}
	redisLocker := utils.NewRedisDL(rdb)
	// 初始化RabbitMQ的连接
	amqpConn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatal("连接RabbitMQ失败:", err)
	}
	defer amqpConn.Close()
	pub, err := mq.NewPublisher(amqpConn)
	if err != nil {
		log.Fatal("创建Publisher失败:", err)
	}
	defer pub.Close()
	userHandler := handler.NewUserHandler(repoCache, redisLocker, bf, pub)
	r := router.SetupRouter(userHandler)
	if err := r.Run(":8080"); err != nil {
		log.Println("启动失败", err)
	}
	log.Println("启动成功!")
}
