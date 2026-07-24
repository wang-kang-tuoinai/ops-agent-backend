package cache

import "ops-agent-backend/internal/model"

type CachedUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email" `
	Age      int    `json:"age"`
}

func ToCachedUser(u model.User) CachedUser {
	return CachedUser{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		Age:      u.Age,
	}
}

func (c CachedUser) ToModelUser() model.User {
	return model.User{
		ID:       c.ID,
		Username: c.Username,
		Email:    c.Email,
		Age:      c.Age,
	}
}
