package main

import (
	"acid/db"
	"acid/internal/cache"
	grpcServer "acid/internal/grpc"
	"acid/internal/handlers"
	loggerUtils "acid/internal/logger"
	"acid/internal/repository"
	"acid/internal/server"
	"acid/internal/services"
	"acid/internal/utils"
	pb "acid/proto/acid"
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
	httpServer   *http.Server
	cacheManager *cache.CacheManager
)

func main() {

	hosts := strings.Split(utils.GetEnv("HOSTS", "localhost"), ",")
	keyspace := utils.GetEnv("KEYSPACE", "acid_data")

	// Initialize database
	database, err := db.Connect(hosts, keyspace)
	if err != nil {
		panic("Failed to connect to database: " + err.Error())
	}
	defer database.Close()

	if err := database.Health(); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	// Initialize logger
	logger, err := loggerUtils.InitLogger()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}

	// Initialize Cache System (Local + Redis)
	cacheManager, err = initializeCacheSystem(logger)
	if err != nil {
		logger.Warn("Failed to initialize cache system, continuing without cache", zap.Error(err))
		// Continue without cache - graceful degradation
	} else {
		defer cacheManager.Close()
		logger.Info("✅ Cache system initialized successfully")
	}

	grpcPort := utils.GetEnv("GRPC_PORT", "50051")
	httpPort := utils.GetEnv("HTTP_PORT", "8000")

	grpcServerInstance := grpc.NewServer()
	router := gin.Default()

	// Initialize repository, service, and handler
	userRepository := repository.NewUserRepository(database.Session)
	userService := services.NewUserService(userRepository, logger, cacheManager)
	userHandler := handlers.NewUserHandler(userService)
	server.SetupRoutes(router, userHandler)

	// Register gRPC service
	acidServer := grpcServer.NewAcidServer(userService, logger)
	pb.RegisterAcidServer(grpcServerInstance, acidServer)
	logger.Info("✅ gRPC Acid service registered")

	go StartGRPCServer(grpcServerInstance, grpcPort, logger)
	go startHTTPServer(httpPort, router, logger)

	<-utils.GracefulShutdown()
	logger.Info("Shutting down servers...")
	shutdownServers(grpcServerInstance, logger)
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

func initializeCacheSystem(logger *zap.Logger) (*cache.CacheManager, error) {
	// Read cache configuration from environment
	redisHost := utils.GetEnv("REDIS_HOST", "localhost")
	redisPort := utils.GetEnv("REDIS_PORT", "6379")
	redisPassword := utils.GetEnv("REDIS_PASSWORD", "")
	enableLocalCache := utils.GetEnv("ENABLE_LOCAL_CACHE", "true") == "true"
	enableRedisCache := utils.GetEnv("ENABLE_REDIS_CACHE", "true") == "true"

	logger.Info("Initializing cache system",
		zap.String("redis_host", redisHost),
		zap.String("redis_port", redisPort),
		zap.Bool("local_cache", enableLocalCache),
		zap.Bool("redis_cache", enableRedisCache),
	)

	var localCache *cache.LocalCache
	var redisClient *cache.RedisClient

	// Initialize local cache (BigCache)
	if enableLocalCache {
		localConfig := &cache.LocalCacheConfig{
			Shards:             1024,
			LifeWindow:         1 * time.Minute,
			CleanWindow:        5 * time.Minute,
			MaxEntriesInWindow: 600000, // 10K entries/sec * 60 sec
			MaxEntrySize:       500,
			HardMaxCacheSize:   100, // 100MB max
			Verbose:            false,
			Name:               "main",
		}

		var err error
		localCache, err = cache.NewLocalCache(localConfig)
		if err != nil {
			logger.Warn("Failed to initialize local cache", zap.Error(err))
			localCache = nil
		} else {
			logger.Info("✅ Local cache initialized")
		}
	}

	// Initialize Redis cache
	if enableRedisCache {
		redisConfig := &cache.RedisConfig{
			Host:         redisHost,
			Port:         redisPort,
			Password:     redisPassword,
			DB:           0,
			MaxRetries:   3,
			PoolSize:     20,
			MinIdleConns: 10,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		}

		var err error
		redisClient, err = cache.NewRedisClient(redisConfig)
		if err != nil {
			logger.Warn("Failed to initialize Redis cache", zap.Error(err))
			redisClient = nil
		} else {
			logger.Info("✅ Redis cache initialized")
		}
	}

	// Create cache manager
	cacheConfig := &cache.CacheManagerConfig{
		LocalTTL:            1 * time.Minute,
		RedisTTL:            10 * time.Minute,
		EnableLocalCache:    localCache != nil,
		EnableRedisCache:    redisClient != nil,
		GracefulDegradation: true, // Continue even if Redis is down
		WriteThrough:        true,
		Name:                "main",
	}

	cacheManager := cache.NewCacheManager(localCache, redisClient, cacheConfig)

	// Verify cache health
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health := cacheManager.HealthCheck(ctx)
	logger.Info("Cache health check", zap.Any("health", health))

	return cacheManager, nil
}

func shutdownServers(grpcServer *grpc.Server, logger *zap.Logger) {
	// Shutdown cache system
	if cacheManager != nil {
		logger.Info("Shutting down cache system...")
		if err := cacheManager.Close(); err != nil {
			logger.Error("❌ Cache system shutdown error", zap.Error(err))
		} else {
			logger.Info("✅ Cache system stopped gracefully")
		}
	}

	// Shutdown gRPC server
	grpcServer.GracefulStop()
	logger.Info("✅ gRPC Server stopped gracefully")

	// Shutdown HTTP server
	if httpServer != nil {
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logger.Error("❌ HTTP server shutdown error", zap.Error(err))
		} else {
			logger.Info("✅ HTTP Server stopped gracefully")
		}
	}
}
