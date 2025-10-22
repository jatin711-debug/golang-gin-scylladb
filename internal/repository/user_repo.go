package repository

import (
	"acid/internal/models"
	"fmt"

	"github.com/gocql/gocql"
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

func (r *UserRepository) GetUserByID(id string) (*models.User, error) {
	var user models.User

	// Convert string ID to UUID
	uuid, err := gocql.ParseUUID(id)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID format: %w", err)
	}

	q := r.session.Query(UserTable.Get()).BindMap(map[string]interface{}{
		"id": uuid,
	})

	if err := q.GetRelease(&user); err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &user, nil
}
