package main

import (
	"log"
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/repository/memory"
	"ops-agent-backend/internal/router"
)

func main() {
	repo := memory.NewUserMemoryRepository()
	userHandler := handler.NewUserHandler(repo)
	r := router.SetupRouter(userHandler)
	if err := r.Run(":8080"); err != nil {
		log.Println("启动失败", err)
	}
	log.Println("启动成功!")
}
