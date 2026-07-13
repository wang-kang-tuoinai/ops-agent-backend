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
	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.Mutex
}

func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.ExchangeDeclare(
		ExchangeUser,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return nil, fmt.Errorf("Declare exchange %s failed:%w", ExchangeUser, err)
	}
	return &Publisher{conn: conn, channel: ch}, nil
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

	return p.channel.PublishWithContext(pubCtx,
		ExchangeUser,
		RoutingKeyUserRegister,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			MessageId:   event.EventId,
			Timestamp:   event.Timestamp,
			Body:        body,
		})
}

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		return p.channel.Close()
	}
	return nil
}
