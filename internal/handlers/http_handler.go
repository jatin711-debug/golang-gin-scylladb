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
