package model

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Age      int    `json:"age,omitempty"`
	Password string `json:"-"`
}
