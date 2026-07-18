package model

type User struct {
	ID       int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Username string `json:"username" binding:"required" gorm:"type:varchar(50);uniqueIndex;not null"`
	Email    string `json:"email" binding:"required,email" gorm:"type:varchar(100);uniqueIndex; not null"`
	Age      int    `json:"age,omitempty" gorm:"type:tinyint;default:0"`
	Password string `gorm:"type:varchar(255);not null"`
}

type UserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email" `
	Age      int    `json:"age,omitempty"`
}

type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Age      *int   `json:"age,omitempty"`
	Password string `json:"password" binding:"required"`
}

type UpdateUserRequest struct {
	Username *string `json:"username"`
	Email    *string `json:"email" binding:"omitempty,email"`
	Age      *int    `json:"age"`
}

func ToUserResponse(u User) UserResponse {
	return UserResponse{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		Age:      u.Age,
	}
}
