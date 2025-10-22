package repository

import (
	"acid/internal/models"

	"github.com/scylladb/gocqlx/v3"
	"github.com/scylladb/gocqlx/v3/table"
)

var UserTable = table.New(table.Metadata{
	Name:    "users",
	Columns: []string{"id", "username", "email", "created_at"},
	PartKey: []string{"id"},
	SortKey: []string{},
})

type UserRepository struct {
	session gocqlx.Session
}

func NewUserRepository(session gocqlx.Session) *UserRepository {
	return &UserRepository{session: session}
}

func (r *UserRepository) CreateUser(user *models.User) error {
	q := r.session.Query(UserTable.Insert()).BindStruct(user)
	if err := q.ExecRelease(); err != nil {
		return err
	}
	return nil
}
