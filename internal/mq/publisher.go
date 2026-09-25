package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	worker *worker
	mu     sync.Mutex
}

// Publisher 拥有连接，Close 会同时停止重连并关闭当前连接。
func NewPublisher(addr string) (*Publisher, error) {
	w, err := startWorker(context.Background(), "publisher", func(ctx context.Context) (*session, error) {
		return openSession(ctx, addr, nil)
	}, waitSession, time.Second)
	if err != nil {
		return nil, err
	}
	return &Publisher{worker: w}, nil
}

func (p *Publisher) PublishUserRegister(ctx context.Context, event UserRegisterEvent) error {
	if event.EventId == "" {
		event.EventId = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	event.EventType = RoutingKeyUserRegister
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	pubCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	p.mu.Lock()
	defer p.mu.Unlock()

	ch, err := p.worker.channel()
	if err != nil {
		return err
	}
	// 不自动重发：发送报错时，消息是否已到达 broker 可能无法确定。
	if err := ch.PublishWithContext(pubCtx,
		ExchangeUser,
		RoutingKeyUserRegister,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			MessageId:   event.EventId,
			Timestamp:   event.Timestamp,
			Body:        body,
		}); err != nil {
		return fmt.Errorf("publish user registration: %w", err)
	}
	return nil
}

func (p *Publisher) Close() error {
	return p.worker.Close()
}
