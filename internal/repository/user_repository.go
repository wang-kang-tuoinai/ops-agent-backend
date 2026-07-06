package repository

import (
	"ops-agent-backend/internal/model"
)

type UserRepository interface {
	Create(u model.User) model.User
	GetById(id int64) (model.User, error)
	GetAll() []model.User
	Update(id int64, u model.User) (model.User, error)
	 Delete(id int64) error
}
