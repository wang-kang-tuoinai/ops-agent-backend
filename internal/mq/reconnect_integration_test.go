//go:build integration

package mq

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// 显式运行：go test -tags=integration -run TestRabbitMQRecovery -v ./internal/mq
// 创建独立 RabbitMQ 容器及随机本地端口，退出时只清理本测试的容器和卷。
func TestRabbitMQRecovery(t *testing.T) {
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// 显式固定选出的空闲端口，避免 Docker 重启时重新分配随机 host port。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hostAddr := listener.Addr().String()
	_ = listener.Close()
	name := "ops-mq-reconnect-test-" + uuid.NewString()
	docker("run", "--detach", "--name", name, "--publish", hostAddr+":5672", "rabbitmq:3-management")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "docker", "rm", "--force", "--volumes", name).CombinedOutput(); err != nil {
			t.Errorf("cleanup test container: %v: %s", err, out)
		}
	})
	addr := "amqp://guest:guest@" + hostAddr + "/"
	wait := func(description string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", description)
	}
	var p *Publisher
	wait("initial broker startup", func() bool {
		var err error
		p, err = NewPublisher(addr)
		return err == nil
	})
	t.Cleanup(func() { _ = p.Close() })
	c := NewConsumer(addr)
	t.Cleanup(func() { _ = c.Close() })
	received := make(chan string, 20)
	queue := "reconnect-test-" + uuid.NewString()
	if err := c.Subscribe(context.Background(), queue, RoutingKeyUserRegister, func(ctx context.Context, e UserRegisterEvent) error {
		received <- e.EventId
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ready := func() bool {
		_, pe := p.worker.channel()
		_, ce := c.worker.channel()
		return pe == nil && ce == nil
	}
	assertDelivery := func() {
		t.Helper()
		id := uuid.NewString()
		if err := p.PublishUserRegister(context.Background(), UserRegisterEvent{EventId: id, UserId: 1}); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-received:
			if got != id {
				t.Fatalf("unexpected/duplicate message: %s, want %s", got, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("message not consumed")
		}
	}
	assertDelivery()
	for i := 0; i < 2; i++ {
		t.Logf("broker restart cycle %d", i+1)
		docker("stop", "--time", "1", name)
		wait("both clients notice disconnect", func() bool {
			_, pe := p.worker.channel()
			_, ce := c.worker.channel()
			return errors.Is(pe, ErrUnavailable) && errors.Is(ce, ErrUnavailable)
		})
		start := time.Now()
		if err := p.PublishUserRegister(context.Background(), UserRegisterEvent{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("offline publish must fail: %v", err)
		}
		if time.Since(start) > 200*time.Millisecond {
			t.Fatal("offline publish waited for recovery")
		}
		docker("start", name)
		wait("publisher and consumer recover", ready)
		assertDelivery()
	}
	// 仅 Channel 关闭也应恢复，不能只监听整个连接。
	ch, err := p.worker.channel()
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	wait("publisher channel recreation", func() bool {
		current, err := p.worker.channel()
		return err == nil && current != ch
	})
	assertDelivery()
	// 删除本测试的队列，触发 basic.cancel，验证重新声明队列和绑定。
	admin, err := amqp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	adminCh, err := admin.Channel()
	if err != nil {
		t.Fatal(err)
	}
	old, err := c.worker.channel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminCh.QueueDelete(queue, false, false, false); err != nil {
		t.Fatal(err)
	}
	wait("subscription and topology recreation", func() bool {
		current, err := c.worker.channel()
		return err == nil && current != old
	})
	assertDelivery()
	q, err := adminCh.QueueInspect(queue)
	if err != nil || q.Consumers != 1 {
		t.Fatalf("expected exactly one consumer: queue=%+v err=%v", q, err)
	}
	// Broker 不可用时退出，也不能被重连循环拖住。
	_ = admin.Close()
	docker("stop", "--time", "1", name)
	start := time.Now()
	_ = c.Close()
	_ = p.Close()
	if time.Since(start) > 3*time.Second {
		t.Fatal("closing disconnected clients took too long")
	}
}
