package mysql

import (
	"context"
	"errors"
	"ops-agent-backend/internal/model"
	"ops-agent-backend/internal/repository"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("repository/mysql")

type UserRepository struct {
	db *gorm.DB
}

func NewUserMysqlRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

func (r *UserRepository) Create(ctx context.Context, u model.User) (model.User, error) {
	ctx, span := tracer.Start(ctx, "mysql.Create")
	defer span.End()
	createCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := r.db.WithContext(createCtx).Create(&u).Error; err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create failed")
		return model.User{}, err
	}
	span.SetAttributes(attribute.Int64("user.id", u.ID))
	return u, nil
}

func (r *UserRepository) GetById(ctx context.Context, id int64) (model.User, error) {
	ctx, span := tracer.Start(ctx, "mysql.GetById")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	var u model.User
	getCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if err := r.db.WithContext(getCtx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			span.SetAttributes(attribute.Bool("user.isExist", false))
			return model.User{}, repository.ErrUserNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "query failed")
		return model.User{}, err
	}
	return u, nil
}

func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]model.User, error) {
	ctx, span := tracer.Start(ctx, "mysql.List")
	defer span.End()
	span.SetAttributes(attribute.Int("offset", offset), attribute.Int("limit", limit))
	var users []model.User
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.db.WithContext(listCtx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list failed")
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
	ctx, span := tracer.Start(ctx, "mysql.Update")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.db.WithContext(updateCtx).Save(&u).Error; err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update failed")
		return model.User{}, err
	}
	return u, nil
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	ctx, span := tracer.Start(ctx, "mysql.Delete")
	defer span.End()
	span.SetAttributes(attribute.Int64("user.id", id))
	deleteCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	result := r.db.WithContext(deleteCtx).Delete(&model.User{}, id)
	if result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, "delete failed")
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repository.ErrUserNotFound
	}
	return nil
}
