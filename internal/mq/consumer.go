package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

type EventHandler func(ctx context.Context, event UserRegisterEvent) error

type Consumer struct {
	conn *amqp.Connection
	ch   *amqp.Channel
	wg   sync.WaitGroup
}

func NewConsumer(conn *amqp.Connection) (*Consumer, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	// 声明同一个Exchange,保证幂等
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
	return &Consumer{
		conn: conn,
		ch:   ch,
	}, nil
}

func (c *Consumer) Subscribe(
	ctx context.Context,
	queueName string,
	routingKey string,
	handler EventHandler,
) error {
	q, err := c.ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("Declared queue %s:%w", queueName, err)
	}
	if err := c.ch.QueueBind(q.Name, routingKey, ExchangeUser, false, nil); err != nil {
		return fmt.Errorf("bind queue %s to %s with key %s:%w", q.Name,
			ExchangeUser, routingKey, err)
	}

	msgs, err := c.ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume queue %s:%w", q.Name, err)
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		log.Printf("[Consumer] queue=%s rountingKey=:%s 开始监听...\n", queueName, routingKey)
		for {
			select {
			case <-ctx.Done():
				log.Printf("[Consumer] queue=%s 停止消费: %v\n", queueName, ctx.Err())
				return
			case msg, ok := <-msgs:
				if !ok {
					log.Printf("[Consumer] queue=%s channel已关闭\n", queueName)
					return
				}
				var event UserRegisterEvent
				if err := json.Unmarshal(msg.Body, &event); err != nil {
					log.Printf("[Consumer] queue = %s 反序列化失败 %v\n", queueName, err)
					// 如果格式错误直接丢弃
					_ = msg.Nack(false, false)
					continue
				}
				if err := handler(ctx, event); err != nil {
					log.Printf("[Consumer] queue %s 处理失败: evendId= %s err=%v\n",
						queueName, event.EventId, err)
					//处理失败,尝试重新放回队列重试
					_ = msg.Nack(false, true)
					continue
				}
				_ = msg.Ack(false)
				log.Printf("[Consumer] queue = %s 处理成功: eventId=%s\n", queueName, event.EventId)
			}
		}
	}()
	return nil
}

func (c *Consumer) Close() error {
	// 先等待GoRouting真正跑完
	c.wg.Wait()
	if c.ch != nil {
		return c.ch.Close()
	}
	return nil
}
