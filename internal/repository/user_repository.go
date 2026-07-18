package repository

import (
	"context"
	"ops-agent-backend/internal/model"
)

type UserRepository interface {
	Create(ctx context.Context, u model.User) (model.User, error)
	GetById(ctx context.Context, id int64) (model.User, error)
	List(ctx context.Context, offset, limit int) ([]model.User, error)
	ListAllIDs(ctx context.Context) ([]int64, error)
	Update(ctx context.Context, id int64, u model.User) (model.User, error)
	Delete(ctx context.Context, id int64) error
}
