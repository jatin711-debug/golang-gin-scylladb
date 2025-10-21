package main

import (
	"acid/db"
	loggerUtils "acid/internal/logger"
	"acid/internal/utils"
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var (
	httpServer *http.Server
)

func main() {

	hosts := strings.Split(utils.GetEnv("HOSTS", "localhost"), ",")
	keyspace := utils.GetEnv("KEYSPACE", "acid_data")

	database, err := db.Connect(hosts, keyspace)
	if err != nil {
		panic("Failed to connect to database: " + err.Error())
	}
	defer database.Close()

	if err := database.Health(); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	logger, err := loggerUtils.InitLogger()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	grpcPort := utils.GetEnv("GRPC_PORT", "50051")
	httpPort := utils.GetEnv("HTTP_PORT", "8000")

	grpcServer := grpc.NewServer()
	router := gin.Default()
	gin.SetMode(gin.ReleaseMode)

	go StartGRPCServer(grpcServer, grpcPort, logger)
	go startHTTPServer(httpPort, router, logger)

	<-utils.GracefulShutdown()
	logger.Info("Shutting down servers...")
	shutdownServers(grpcServer, logger)
}

func StartGRPCServer(grpcServer *grpc.Server, port string, logger *zap.Logger) {
	logger.Info("Starting gRPC server on port " + port)
	// gRPC server setup and start logic goes here
	listerner, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Fatal("Failed to listen on port " + port + ": " + err.Error())
	}
	if err := grpcServer.Serve(listerner); err != nil {
		logger.Fatal("Failed to serve gRPC server: " + err.Error())
	}
}

func startHTTPServer(port string, router *gin.Engine, logger *zap.Logger) {
	logger.Info("Starting HTTP server on port " + port)
	httpServer = &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		logger.Fatal("Failed to serve HTTP server: " + err.Error())
	}
}

func shutdownServers(grpcServer *grpc.Server, logger *zap.Logger) {
	grpcServer.GracefulStop()
	logger.Info("✅ gRPC Server stopped gracefully")

	if httpServer != nil {
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logger.Error("❌ HTTP server shutdown error", zap.Error(err))
		} else {
			logger.Info("✅ HTTP Server stopped gracefully")
		}
	}
}
