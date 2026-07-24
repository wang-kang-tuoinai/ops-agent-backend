package memory

import (
	"context"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"slices"
	"sync"
)

type UserRepository struct {
	mu     sync.RWMutex
	users  map[int64]model.User
	nextID int64
}

func NewUserMemoryRepository() *UserRepository {
	return &UserRepository{
		users:  make(map[int64]model.User),
		nextID: 1,
	}
}

// 创建用户
func (r *UserRepository) Create(ctx context.Context, u model.User) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u.ID = r.nextID
	r.nextID++
	r.users[u.ID] = u
	return u, nil
}

// 根据id获取用户
func (r *UserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	if !ok {
		return model.User{}, repository.ErrUserNotFound
	}
	return u, nil
}

// 获取所有用户
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]int64, 0, len(r.users))
	for key := range r.users {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	total := len(keys)
	if offset < 0 || offset >= total {
		return []model.User{}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}

	pageKeys := keys[offset:end]
	var result []model.User
	for _, key := range pageKeys {
		result = append(result, r.users[key])
	}
	return result, nil
}

func (r *UserRepository) ListAllIDs(ctx context.Context) ([]int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]int64, 0, len(r.users))
	for k := range r.users {
		keys = append(keys, k)
	}
	return keys, nil
}

// 更新用户信息
func (r *UserRepository) Update(ctx context.Context, id int64, upd model.UserUpdate) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return model.User{}, repository.ErrUserNotFound
	}
	if upd.Age != nil {
		u.Age = *upd.Age
	}
	if upd.Email != nil {
		u.Email = *upd.Email
	}
	if upd.Username != nil {
		u.Username = *upd.Username
	}
	r.users[id] = u
	return u, nil
}

// 删除用户
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.users[id]
	if !ok {
		return repository.ErrUserNotFound
	}
	delete(r.users, id)
	return nil
}
