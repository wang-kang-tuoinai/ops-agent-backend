package mq

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ErrUnavailable 表示当前没有可用于发布的 Channel，包括恢复中或已停止。
var ErrUnavailable = errors.New("RabbitMQ unavailable")

// 每代 session 都拥有独立的连接、Channel 和订阅，旧消息只能在旧 Channel 上确认。
type session struct {
	channel    *amqp.Channel
	deliveries <-chan amqp.Delivery
	connClosed <-chan *amqp.Error
	chanClosed <-chan *amqp.Error
	cancelled  <-chan string
	close      func()
}

type setupChannel func(*amqp.Channel) (<-chan amqp.Delivery, error)

func openSession(ctx context.Context, addr string, setup setupChannel) (*session, error) {
	var socket net.Conn
	var stopCancel func() bool
	defer func() {
		if stopCancel != nil {
			stopCancel()
		}
	}()
	conn, err := amqp.DialConfig(addr, amqp.Config{
		Heartbeat: 5 * time.Second,
		// 不启用库的 Recovery，由 worker 统一恢复连接和业务拓扑。
		Dial: func(network, address string) (net.Conn, error) {
			// 建立底层TCP连接
			dialer := net.Dialer{Timeout: 5 * time.Second}
			c, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			socket = c
			stopCancel = context.AfterFunc(ctx, func() { _ = c.Close() })
			// 给握手设置5s超时,防止一直阻塞
			if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				_ = c.Close()
				return nil, err
			}
			return c, nil
		},
	})
	if err != nil {
		if socket != nil {
			_ = socket.Close()
		}
		return nil, err
	}
	closeConn := func() { _ = conn.CloseDeadline(time.Now().Add(time.Second)) }
	// 心跳会更新读 deadline，因此用独立计时器限制拓扑声明的总等待时间。
	setupTimer := time.AfterFunc(5*time.Second, func() { _ = socket.Close() })
	defer setupTimer.Stop()
	ch, err := conn.Channel()
	if err != nil {
		closeConn()
		return nil, err
	}
	s := &session{
		channel: ch, close: closeConn,
		connClosed: conn.NotifyClose(make(chan *amqp.Error, 1)),
		chanClosed: ch.NotifyClose(make(chan *amqp.Error, 1)),
		cancelled:  ch.NotifyCancel(make(chan string, 1)),
	}
	if err = ch.ExchangeDeclare(ExchangeUser, "topic", true, false, false, false, nil); err == nil && setup != nil {
		s.deliveries, err = setup(ch)
	}
	if err == nil {
		err = ctx.Err()
	}
	if !setupTimer.Stop() && err == nil {
		err = context.DeadlineExceeded
	}
	if err != nil {
		closeConn()
		return nil, err
	}
	return s, nil
}

type sessionOpener func(context.Context) (*session, error)
type sessionServer func(context.Context, *session) error

// worker 是唯一的恢复循环；网络操作和退避期间不持有 mu。
type worker struct {
	mu      sync.RWMutex
	current *session
	err     error
	cancel  context.CancelFunc
	done    chan struct{}
}

func startWorker(parent context.Context, label string, open sessionOpener, serve sessionServer, retryDelay time.Duration) (*worker, error) {
	ctx, cancel := context.WithCancel(parent)
	s, err := open(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	w := &worker{current: s, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer cancel()
		for {
			err := serve(ctx, s)
			w.mu.Lock()
			w.current = nil
			w.err = err
			w.mu.Unlock()
			s.close()
			if ctx.Err() != nil {
				return
			}
			log.Printf("[RabbitMQ:%s] 连接或订阅中断: %v", label, err)
			for delay := retryDelay; ; delay = min(delay*2, 30*time.Second) {
				if permanentError(err) {
					log.Printf("[RabbitMQ:%s] 配置/协议错误，停止恢复，请修正配置后重启: %v", label, err)
					return
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				s, err = open(ctx)
				if err != nil {
					w.mu.Lock()
					w.err = err
					w.mu.Unlock()
					log.Printf("[RabbitMQ:%s] 重连失败: %v", label, err)
					continue
				}
				if ctx.Err() != nil {
					s.close()
					return
				}
				w.mu.Lock()
				w.current, w.err = s, nil
				w.mu.Unlock()
				log.Printf("[RabbitMQ:%s] 连接及拓扑恢复成功", label)
				break
			}
		}
	}()
	return w, nil
}

func (w *worker) channel() (*amqp.Channel, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.current == nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, w.err)
	}
	ch := w.current.channel
	if ch.IsClosed() {
		return nil, ErrUnavailable
	}
	return ch, nil
}

func (w *worker) Close() error {
	w.cancel()
	<-w.done
	return nil
}

func waitSession(ctx context.Context, s *session) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-s.connClosed:
		return closeReason(err)
	case err := <-s.chanClosed:
		return closeReason(err)
	}
}

func closeReason(err *amqp.Error) error {
	if err != nil {
		return err
	}
	return amqp.ErrClosed
}

func permanentError(err error) bool {
	var e *amqp.Error
	if !errors.As(err, &e) || !e.Server {
		return false
	}
	switch e.Code {
	case amqp.AccessRefused, amqp.NotFound, amqp.ResourceLocked, amqp.PreconditionFailed,
		amqp.SyntaxError, amqp.CommandInvalid, amqp.NotAllowed, amqp.NotImplemented:
		return true
	}
	return false
}
