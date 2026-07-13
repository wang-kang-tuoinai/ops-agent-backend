package mq

import (
	"time"
)

const (
	ExchangeUser           = "user.event"
	RoutingKeyUserRegister = "user.register"
)

type UserRegisterEvent struct {
	EventId   string    `json:"event_id"`
	TraceID   string    `json:"trace_id"`
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`

	UserId   int64  `json:"user_id"`
	UserName string `json:"user_name"`
}
