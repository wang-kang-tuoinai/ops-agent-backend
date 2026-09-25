package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type EventHandler func(ctx context.Context, event UserRegisterEvent) error

// 每个 Consumer 管理一个订阅；需要多个订阅时创建多个 Consumer。
type Consumer struct {
	addr   string
	mu     sync.Mutex
	worker *worker
	closed bool
}

// 连接在 Subscribe 中建立，只有队列声明和订阅均成功才开始消费。
func NewConsumer(addr string) *Consumer {
	return &Consumer{addr: addr}
}

func (c *Consumer) Subscribe(ctx context.Context, queueName, routingKey string, handler EventHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("consumer is closed")
	}
	if c.worker != nil {
		return errors.New("consumer already subscribed")
	}
	if handler == nil {
		return errors.New("consumer handler is nil")
	}
	w, err := startWorker(ctx, "consumer:"+queueName, func(ctx context.Context) (*session, error) {
		return openSession(ctx, c.addr, func(ch *amqp.Channel) (<-chan amqp.Delivery, error) {
			q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
			if err != nil {
				return nil, fmt.Errorf("declare queue %s: %w", queueName, err)
			}
			if err := ch.QueueBind(q.Name, routingKey, ExchangeUser, false, nil); err != nil {
				return nil, fmt.Errorf("bind queue %s: %w", q.Name, err)
			}
			return ch.Consume(q.Name, "", false, false, false, false, nil)
		})
	}, func(ctx context.Context, s *session) error {
		return consumeSession(ctx, s, queueName, handler)
	}, time.Second)
	if err != nil {
		return err
	}
	c.worker = w
	return nil
}

func consumeSession(ctx context.Context, s *session, queueName string, handler EventHandler) error {
	log.Printf("[Consumer] queue=%s 开始监听", queueName)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.connClosed:
			return closeReason(err)
		case err := <-s.chanClosed:
			return closeReason(err)
		case tag := <-s.cancelled:
			return fmt.Errorf("subscription cancelled by broker: %s", tag)
		case msg, ok := <-s.deliveries:
			if !ok {
				// 尽量保留 Channel 关闭的协议错误原因。
				select {
				case err := <-s.chanClosed:
					return closeReason(err)
				default:
					return amqp.ErrClosed
				}
			}
			var event UserRegisterEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				log.Printf("[Consumer] queue=%s 反序列化失败: %v", queueName, err)
				if err := msg.Nack(false, false); err != nil {
					return err
				}
				continue
			}
			if err := handler(ctx, event); err != nil {
				log.Printf("[Consumer] queue=%s 处理失败: eventId=%s err=%v", queueName, event.EventId, err)
				if ctx.Err() != nil {
					return ctx.Err() // 关闭连接后，未确认消息由 broker 重新入队。
				}
				// 保留原有策略：处理失败重新入队，重试次数限制另行处理。
				if err := msg.Nack(false, true); err != nil {
					return err
				}
				continue
			}
			if err := msg.Ack(false); err != nil {
				return err
			}
			log.Printf("[Consumer] queue=%s 处理成功: eventId=%s", queueName, event.EventId)
		}
	}
}

func (c *Consumer) Close() error {
	c.mu.Lock()
	c.closed = true
	w := c.worker
	c.mu.Unlock()
	if w != nil {
		return w.Close()
	}
	return nil
}
