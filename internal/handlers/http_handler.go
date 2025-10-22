package handlers

import (
	"acid/internal/models"
	"acid/internal/services"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type UserHandler struct {
	service *services.UserService
}

func NewUserHandler(service *services.UserService) *UserHandler {
	return &UserHandler{
		service: service,
	}
}

func (h *UserHandler) HealthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": "healthy",
	})
}

func (h *UserHandler) CreateUser(c *gin.Context) {
	// Logic to create a user goes here
	var userRequest models.UserRequest
	if err := c.ShouldBindJSON(&userRequest); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	user, err := models.NewUser(userRequest.Username, userRequest.Email)

	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to create user"})
		return
	}

	h.service.Logger.Info("Creating user", zap.String("username", user.Username))
	if err := h.service.Repo.CreateUser(user); err != nil {
		h.service.Logger.Error("Failed to save user to database", zap.Error(err))
		c.JSON(500, gin.H{"error": "Failed to save user to database"})
		return
	}
	// Here you would typically call h.service to save the user to the database
	c.JSON(201, gin.H{
		"message": "User created successfully",
		"user":    user,
	})
}

func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	h.service.Logger.Info("Getting user", zap.String("id", id))

	var user models.User

	// Try to get from cache using GetOrSetJSON
	source, err := h.service.CacheManager.GetOrSetJSON(
		c.Request.Context(),
		"user:"+id,
		&user,
		func() (interface{}, error) {
			// This function is only called on cache miss
			h.service.Logger.Info("Fetching user from database", zap.String("id", id))
			fetchedUser, dbErr := h.service.Repo.GetUserByID(id)
			if dbErr != nil {
				h.service.Logger.Error("Database fetch failed",
					zap.String("id", id),
					zap.Error(dbErr))
				return nil, dbErr
			}
			h.service.Logger.Info("User fetched from database successfully",
				zap.String("id", id),
				zap.String("username", fetchedUser.Username))
			return fetchedUser, nil
		},
	)

	if err != nil {
		h.service.Logger.Error("Failed to get user",
			zap.String("id", id),
			zap.Error(err))
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	h.service.Logger.Info("User retrieved successfully",
		zap.String("id", id),
		zap.String("username", user.Username),
		zap.String("source", source))

	c.JSON(200, gin.H{
		"user":   user,
		"source": source,
	})
}

// GetCacheMetrics returns cache performance metrics
func (h *UserHandler) GetCacheMetrics(c *gin.Context) {
	metrics := h.service.CacheManager.GetMetrics()
	health := h.service.CacheManager.HealthCheck(c.Request.Context())

	c.JSON(200, gin.H{
		"metrics": metrics,
		"health":  health,
	})
}
