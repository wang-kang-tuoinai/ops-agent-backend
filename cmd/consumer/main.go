package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"ops-agent-backend/internal/mq"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// TODO RabbitMQ 重启后,消费者会停止消费
func main() {
	// 初始化RabbitMQ的连接
	amqp_addr := getEnv("RABBITMQ_ADDR", "amqp://guest:guest@localhost:5672/")
	var amqpConn *amqp.Connection
	err := withRetry("RabbitMQ", 5, 4*time.Second, func() error {
		var dialErr error
		amqpConn, dialErr = amqp.DialConfig(amqp_addr,
			amqp.Config{Dial: func(network string, addr string) (net.Conn, error) {
				return net.DialTimeout(network, addr, 5*time.Second)
			}},
		)
		return dialErr
	},
	)
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
