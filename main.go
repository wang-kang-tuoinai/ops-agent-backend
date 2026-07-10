package main

import (
	"log"
	"ops-agent-backend/internal/cache"
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/repository/memory"
	"ops-agent-backend/internal/router"

	"github.com/redis/go-redis/v9"
)

func main() {
	repo := memory.NewUserMemoryRepository()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	repoCache := cache.NewUserCacheRepository(repo, rdb)
	userHandler := handler.NewUserHandler(repoCache)
	r := router.SetupRouter(userHandler)
	if err := r.Run(":8080"); err != nil {
		log.Println("启动失败", err)
	}
	log.Println("启动成功!")
}
