package main

import (
	"context"
	"fmt"
	"log"
	"ops-agent-backend/internal/bloom"
	"ops-agent-backend/internal/cache"
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/mq"
	"ops-agent-backend/internal/repository/mysql"
	"ops-agent-backend/internal/router"
	"ops-agent-backend/internal/utils"
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	dsn := "root:root@tcp(127.0.0.1:3306)/ops_agent?charset=utf8mb4&parseTime=True&loc=Local"
	dsn = getEnv("MYSQL_DSN", dsn)
	db, err := gorm.Open(mysqlDriver.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("连接mysql失败:", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		log.Fatal("创建mysql表失败:", err)
	}
	repo := mysql.NewUserMysqlRepository(db)
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	repoCache := cache.NewUserCacheRepository(repo, rdb)
	bf := bloom.NewBloomFilter(10000, 4)
	userIDs, err := repoCache.ListAllIDs(context.Background())
	if err != nil {
		log.Println("Get all users failed:", err)
	}
	for _, id := range userIDs {
		bf.Add(fmt.Sprintf("%d", id))
	}
	redisLocker := utils.NewRedisDL(rdb)
	// 初始化RabbitMQ的连接
	rabbitmqAddr := getEnv("RABBITMQ_ADDR", "amqp://guest:guest@localhost:5672/")
	amqpConn, err := amqp.Dial(rabbitmqAddr)
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

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
