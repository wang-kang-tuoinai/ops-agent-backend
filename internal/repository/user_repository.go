package repository

import (
	"errors"
	"ops-agent-backend/internal/model"
	"sync"
)

type UserRepository struct {
	mu     sync.RWMutex
	users  map[int64]model.User
	nextID int64
}

func NewUserRepository() *UserRepository {
	return &UserRepository{
		users:  make(map[int64]model.User),
		nextID: 1,
	}
}

// 创建用户
func (r *UserRepository) Create(u model.User) model.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	u.ID = r.nextID
	r.nextID++
	r.users[u.ID] = u
	return u
}

// 根据id获取用户
func (r *UserRepository) GetById(id int64) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	if !ok {
		return model.User{}, errors.New("user not found")
	}
	return u, nil
}

// 获取所有用户
func (r *UserRepository) GetAll() []model.User {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]model.User, 0, len(r.users))
	for _, u := range r.users {
		list = append(list, u)
	}
	return list
}

// 更新用户信息
func (r *UserRepository) Update(id int64,u model.User) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.users[id]
	if !ok {
		return model.User{}, errors.New("user not found!")
	}
	r.users[id] = u
	return u, nil
}

// 删除用户
func (r *UserRepository) Delete(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.users[id]
	if !ok {
		return errors.New("user not found!")
	}
	delete(r.users, id)
	return nil
}
