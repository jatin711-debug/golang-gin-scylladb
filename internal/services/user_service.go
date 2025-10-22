package services

import (
	"acid/internal/repository"
	"acid/internal/cache"
	"go.uber.org/zap"
)

type UserService struct {
	Repo        *repository.UserRepository
	Logger      *zap.Logger
	CacheManager *cache.CacheManager
}

func NewUserService(repo *repository.UserRepository, logger *zap.Logger, cacheManager *cache.CacheManager) *UserService {
	return &UserService{
		Repo:        repo,
		Logger:      logger,
		CacheManager: cacheManager,
	}
}
