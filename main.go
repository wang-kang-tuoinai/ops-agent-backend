package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"ops-agent-backend/internal/bloom"
	"ops-agent-backend/internal/cache"
	"ops-agent-backend/internal/handler"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/mq"
	"ops-agent-backend/internal/repository/mysql"
	"ops-agent-backend/internal/router"
	"ops-agent-backend/internal/utils"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	//初始化Tracer
	tp, err := initTracer()
	if err != nil {
		log.Fatal("初始化Tracer失败:", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Println("关闭Trace超时或失败:", err)
		}
	}()
	//初始化Mysql
	dsn := "root:root@tcp(127.0.0.1:3306)/ops_agent?charset=utf8mb4&parseTime=True&loc=Local"
	dsn = getEnv("MYSQL_DSN", dsn)
	var db *gorm.DB
	err = withRetry("MySQL", 5, 2*time.Second, func() error {
		var openErr error
		db, openErr = gorm.Open(mysqlDriver.Open(dsn), &gorm.Config{})
		if openErr != nil {
			return openErr
		}
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.Ping(); err != nil {
			sqlDB.Close()
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatal("连接mysql失败:", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		log.Fatal("创建mysql表失败:", err)
	}
	repo := mysql.NewUserMysqlRepository(db)
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{
		Addr:            redisAddr,
		DialTimeout:     5 * time.Second, // 建立连接的超时时间
		ReadTimeout:     3 * time.Second, // 读超时
		WriteTimeout:    3 * time.Second, // 写超时
		MaxRetries:      3,               // 命令执行失败时的最大重试次数
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	})
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
	var amqpConn *amqp.Connection
	err = withRetry("RabbitMQ", 5, 4*time.Second, func() error {
		var dialErr error
		amqpConn, dialErr = amqp.DialConfig(rabbitmqAddr,
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
	pub, err := mq.NewPublisher(amqpConn)
	if err != nil {
		log.Fatal("创建Publisher失败:", err)
	}
	defer pub.Close()
	userHandler := handler.NewUserHandler(repoCache, redisLocker, bf, pub)
	r := router.SetupRouter(userHandler)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("HTTP服务启动失败:", err)
		}
	}()
	log.Println("启动成功!")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("收到信号 %v,开始优雅退出", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Println("关闭HTTP服务失败:", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func initTracer() (*trace.TracerProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//创建OTLP HTTP Exporter
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	//定义当前服务的资源属性
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("ops-agent-backend"),
	))
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
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
