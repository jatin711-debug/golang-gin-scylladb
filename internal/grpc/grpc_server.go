package grpc

import (
	"acid/internal/models"
	"acid/internal/services"
	pb "acid/proto/acid"
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AcidServer implements the gRPC Acid service
type AcidServer struct {
	pb.UnimplementedAcidServer
	userService *services.UserService
	logger      *zap.Logger
}

// NewAcidServer creates a new gRPC server instance
func NewAcidServer(userService *services.UserService, logger *zap.Logger) *AcidServer {
	return &AcidServer{
		userService: userService,
		logger:      logger,
	}
}

// CreateUser implements the createUser RPC method
func (s *AcidServer) CreateUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	s.logger.Info("gRPC CreateUser called",
		zap.String("name", req.Name),
		zap.String("email", req.Email))

	// Validate input
	if req.Name == "" || req.Email == "" {
		s.logger.Warn("Invalid input for CreateUser",
			zap.String("name", req.Name),
			zap.String("email", req.Email))
		return &pb.RegisterUserResponse{
			Response: pb.RegisterUserResponse_FAILURE,
		}, status.Error(codes.InvalidArgument, "name and email are required")
	}

	// Create user model
	user, err := models.NewUser(req.Name, req.Email)
	if err != nil {
		s.logger.Error("Failed to create user model", zap.Error(err))
		return &pb.RegisterUserResponse{
			Response: pb.RegisterUserResponse_FAILURE,
		}, status.Error(codes.Internal, "failed to create user")
	}

	// Check if email already exists (using cache)
	emailKey := "email:" + req.Email
	exists, err := s.userService.CacheManager.Exists(ctx, emailKey)
	if err != nil {
		s.logger.Warn("Failed to check email in cache", zap.Error(err))
		// Continue without cache check (graceful degradation)
	} else if exists {
		s.logger.Warn("Email already exists", zap.String("email", req.Email))
		return &pb.RegisterUserResponse{
			Response: pb.RegisterUserResponse_FAILURE,
		}, status.Error(codes.AlreadyExists, "email already registered")
	}

	// Save to database
	if err := s.userService.Repo.CreateUser(user); err != nil {
		s.logger.Error("Failed to save user to database",
			zap.String("email", req.Email),
			zap.Error(err))
		return &pb.RegisterUserResponse{
			Response: pb.RegisterUserResponse_FAILURE,
		}, status.Error(codes.Internal, "failed to save user")
	}

	// Cache the email for uniqueness check (stores user_id as string)
	// Reuse emailKey from above
	if err := s.userService.CacheManager.Set(ctx, emailKey, user.ID.String()); err != nil {
		s.logger.Warn("Failed to cache email", zap.Error(err))
		// Don't fail the request, user is already created
	}

	// Note: We don't cache the user object here. It will be cached automatically
	// when FetchUser is called via GetOrSetJSON pattern.

	s.logger.Info("User created successfully via gRPC",
		zap.String("id", user.ID.String()),
		zap.String("email", req.Email))

	return &pb.RegisterUserResponse{
		Response: pb.RegisterUserResponse_SUCCESS,
	}, nil
}

// FetchUser implements the fetchUser RPC method
func (s *AcidServer) FetchUser(ctx context.Context, req *pb.FetchUserRequest) (*pb.FetchUserResponse, error) {
	s.logger.Info("gRPC FetchUser called", zap.String("user_id", req.UserId))

	// Validate input
	if req.UserId == "" {
		s.logger.Warn("Empty user_id provided")
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	var user models.User

	// Try to get from cache or database
	source, err := s.userService.CacheManager.GetOrSetJSON(
		ctx,
		"user:"+req.UserId,
		&user,
		func() (interface{}, error) {
			s.logger.Info("Fetching user from database", zap.String("user_id", req.UserId))
			return s.userService.Repo.GetUserByID(req.UserId)
		},
	)

	if err != nil {
		s.logger.Error("Failed to fetch user",
			zap.String("user_id", req.UserId),
			zap.Error(err))
		return nil, status.Error(codes.NotFound, "user not found")
	}

	s.logger.Info("User fetched successfully via gRPC",
		zap.String("user_id", req.UserId),
		zap.String("source", source))

	return &pb.FetchUserResponse{
		Name:  user.Username,
		Email: user.Email,
	}, nil
}
