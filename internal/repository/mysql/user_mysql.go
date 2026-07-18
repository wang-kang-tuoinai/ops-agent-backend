package mysql

import (
	"context"
	"errors"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"time"

	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserMysqlRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (r *UserRepository) Create(ctx context.Context, u model.User) (model.User, error) {
	createCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := r.db.WithContext(createCtx).Create(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *UserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	var u model.User
	getCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if err := r.db.WithContext(getCtx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, repository.ErrUserNotFound
		}
		return model.User{}, err
	}
	return u, nil
}

func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]model.User, error) {
	var users []model.User
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.db.WithContext(listCtx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return []model.User{}, err
	}
	return users, nil
}

func (r *UserRepository) ListAllIDs(ctx context.Context) ([]int64, error) {
	var userIDs []int64
	listAllIdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.db.WithContext(listAllIdCtx).Model(&model.User{}).Pluck("id", &userIDs).Error; err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (r *UserRepository) Update(ctx context.Context, id int64, u model.User) (model.User, error) {
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.db.WithContext(updateCtx).Save(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	deleteCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	result := r.db.WithContext(deleteCtx).Delete(&model.User{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repository.ErrUserNotFound
	}
	return nil
}
