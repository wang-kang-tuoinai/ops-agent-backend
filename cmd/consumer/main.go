package main

import (
	"context"
	"log"
	"ops-agent-backend/internal/mq"
	"os"
	"os/signal"
	"syscall"

	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	// 初始化RabbitMQ的连接
	amqpConn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatal("连接RabbitMQ失败:", err)
	}
	defer amqpConn.Close()
	c, err := mq.NewConsumer(amqpConn)
	if err != nil {
		log.Fatal("Consumer创建失败:", err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Subscribe(ctx, "email-service", mq.RoutingKeyUserRegister, func(ctx context.Context, event mq.UserRegisterEvent) error {
		log.Printf("userId=%d, userName=%s registered at %s", event.UserId, event.UserName, event.Timestamp)
		return nil
	}); err != nil {
		log.Fatal("订阅失败:", err)
	}

	// 阻塞在这里，等待 Ctrl+C 或 kill 信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("收到信号 %v，开始优雅退出...", sig)
	// defer 会自动执行 cancel() → consumer.Close() → amqpConn.Close()
}
