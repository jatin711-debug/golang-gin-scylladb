package services

import (
	"acid/internal/repository"

	"go.uber.org/zap"
)

type UserService struct {
	Repo   *repository.UserRepository
	Logger *zap.Logger
}

func NewUserService(repo *repository.UserRepository, logger *zap.Logger) *UserService {
	return &UserService{
		Repo:   repo,
		Logger: logger,
	}
}
