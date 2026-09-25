package main

import (
	"context"
	"fmt"
	"log"
	"ops-agent-backend/internal/mq"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	rabbitmqAddr := getEnv("RABBITMQ_ADDR", "amqp://guest:guest@localhost:5672/")
	c := mq.NewConsumer(rabbitmqAddr)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := withRetry("RabbitMQ订阅", 5, 4*time.Second, func() error {
		return c.Subscribe(ctx, "email-service", mq.RoutingKeyUserRegister, func(ctx context.Context, event mq.UserRegisterEvent) error {
			log.Printf("userId=%d, userName=%s registered at %s", event.UserId, event.UserName, event.Timestamp)
			return nil
		})
	}); err != nil {
		log.Fatal("订阅失败:", err)
	}

	// 阻塞在这里，等待 Ctrl+C 或 kill 信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("收到信号 %v，开始优雅退出...", sig)
	// cancel 和 Close 会停止消费、重连，并关闭 Consumer 拥有的连接。
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func withRetry(operationName string, maxRetries int, delay time.Duration, fn func() error) error {
	var err error
	for i := 1; i <= maxRetries; i++ {
		if err = fn(); err == nil {
			return nil
		}
		log.Printf("%s连接失败 (第%d/%d次重试),错误%v", operationName, i, maxRetries, err)
		if i < maxRetries {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("[%s] 达到最大重试次数，最终失败: %w", operationName, err)
}
