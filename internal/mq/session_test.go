package mq

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

type testSession struct {
	*session
	conn   chan *amqp.Error
	ch     chan *amqp.Error
	closed atomic.Int32
}

func newTestSession() *testSession {
	s := &testSession{conn: make(chan *amqp.Error, 1), ch: make(chan *amqp.Error, 1)}
	s.session = &session{connClosed: s.conn, chanClosed: s.ch, close: func() { s.closed.Add(1) }}
	return s
}

func TestWorkerRecoversConnectionAndChannel(t *testing.T) {
	var attempts atomic.Int32
	created := make(chan *testSession, 10)
	w, err := startWorker(context.Background(), "test", func(context.Context) (*session, error) {
		n := attempts.Add(1)
		if n == 2 || n == 3 {
			return nil, errors.New("broker still offline")
		}
		s := newTestSession()
		created <- s
		return s.session, nil
	}, waitSession, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	first := <-created
	first.conn <- &amqp.Error{Code: amqp.ConnectionForced, Server: true}
	var second *testSession
	select {
	case second = <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not recovered")
	}
	second.ch <- &amqp.Error{Code: amqp.InternalError, Server: true}
	var third *testSession
	select {
	case third = <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("channel was not recovered")
	}
	eventually(t, func() bool {
		w.mu.RLock()
		defer w.mu.RUnlock()
		return w.current == third.session
	})
	_ = w.Close()
	if attempts.Load() != 5 || first.closed.Load() != 1 || second.closed.Load() != 1 || third.closed.Load() != 1 {
		t.Fatal("recovery did not release exactly one session per generation")
	}
}

func TestWorkerCloseInterruptsBackoff(t *testing.T) {
	s := newTestSession()
	var attempts atomic.Int32
	w, err := startWorker(context.Background(), "test", func(context.Context) (*session, error) {
		attempts.Add(1)
		return s.session, nil
	}, waitSession, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	close(s.conn)
	eventually(t, func() bool { return s.closed.Load() == 1 })
	if _, err := w.channel(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable while disconnected, got %v", err)
	}
	done := make(chan struct{})
	go func() { _ = w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close waited for backoff")
	}
	if attempts.Load() != 1 {
		t.Fatal("Close triggered another dial")
	}
}

func TestWorkerCloseCancelsDial(t *testing.T) {
	s := newTestSession()
	started := make(chan struct{})
	var attempts atomic.Int32
	w, err := startWorker(context.Background(), "test", func(ctx context.Context) (*session, error) {
		if attempts.Add(1) == 1 {
			return s.session, nil
		}
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, waitSession, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	close(s.conn)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("redial did not start")
	}
	done := make(chan struct{})
	go func() { _ = w.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel dial")
	}
}

func TestWorkerDoesNotRetryInvalidTopology(t *testing.T) {
	s := newTestSession()
	var attempts atomic.Int32
	w, err := startWorker(context.Background(), "test", func(context.Context) (*session, error) {
		attempts.Add(1)
		return s.session, nil
	}, waitSession, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	s.ch <- &amqp.Error{Code: amqp.PreconditionFailed, Server: true, Reason: "incompatible exchange"}
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("configuration error should stop recovery")
	}
	if attempts.Load() != 1 {
		t.Fatal("retried a permanent protocol error")
	}
}

func TestOpenSessionCancelsStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := openSession(ctx, "amqp://guest:guest@"+listener.Addr().String()+"/", nil)
		done <- err
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("dial did not reach test server")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled handshake returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt AMQP handshake")
	}
}
