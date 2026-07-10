package repository

import (
	"context"
	"ops-agent-backend/internal/model"
)

type UserRepository interface {
	Create(ctx context.Context, u model.User) (model.User, error)
	GetById(ctx context.Context, id int64) (model.User, error)
	GetAll(ctx context.Context) ([]model.User, error)
	Update(ctx context.Context, id int64, u model.User) (model.User, error)
	Delete(ctx context.Context, id int64) error
}
