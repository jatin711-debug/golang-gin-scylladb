package server

import (
	"acid/internal/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(router *gin.Engine, userHandler *handlers.UserHandler) {
	// Define your HTTP routes here
	gin.SetMode(gin.ReleaseMode)
	api := router.Group("/api/v1")
	{
		api.GET("/health", userHandler.HealthCheck)
		api.POST("/create/user", userHandler.CreateUser)
		api.GET("/get/user/:id", userHandler.GetUser)
	}

}
