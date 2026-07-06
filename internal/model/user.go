package model

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Age      int    `json:"age,omitempty"`
	Password string `json:"-"`
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
