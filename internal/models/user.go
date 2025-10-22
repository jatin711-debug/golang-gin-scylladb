package models

import (
	"fmt"
	"time"

	"github.com/gocql/gocql"
)

type User struct {
	ID        gocql.UUID `db:"id"`
	Username  string     `db:"username"`
	Email     string     `db:"email"`
	CreatedAt time.Time  `db:"created_at"`
}

type UserRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
}

func (u *UserRequest) Validate() error {
	if u.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if u.Email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	return nil
}

func NewUser(username string, email string) (*User, error) {
	uuid := gocql.TimeUUID()
	return &User{
		ID:        uuid,
		Username:  username,
		Email:     email,
		CreatedAt: time.Now(),
	}, nil
}

