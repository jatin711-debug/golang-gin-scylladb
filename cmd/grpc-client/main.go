package main

import (
	pb "acid/proto/acid"
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Connect to gRPC server
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewAcidClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test CreateUser
	log.Println("📝 Testing CreateUser...")
	createResp, err := client.CreateUser(ctx, &pb.RegisterUserRequest{
		Name:  "John Doe",
		Email: "john.doe@example.com",
	})
	if err != nil {
		log.Fatalf("CreateUser failed: %v", err)
	}
	log.Printf("✅ CreateUser response: %v\n", createResp.Response)

	// Wait a bit to ensure data is persisted
	time.Sleep(1 * time.Second)

	// Test FetchUser (you'll need to replace USER_ID with actual UUID from ScyllaDB)
	log.Println("\n📖 Testing FetchUser...")
	log.Println("⚠️  Note: Update USER_ID in this code with an actual user ID from your database")

	// Example - replace this with actual user ID
	// fetchResp, err := client.FetchUser(ctx, &pb.FetchUserRequest{
	// 	UserId: "YOUR-UUID-HERE",
	// })
	// if err != nil {
	// 	log.Fatalf("FetchUser failed: %v", err)
	// }
	// log.Printf("✅ FetchUser response: name=%s, email=%s\n", fetchResp.Name, fetchResp.Email)

	log.Println("\n✅ gRPC client test completed!")
}
