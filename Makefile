postgres:
	docker run --name postgres-database -e POSTGRES_USER=root -e POSTGRES_PASSWORD=secret -p 5432:5432 -d postgres:17	

createdb:
	docker exec -it docker-postgres-1 createdb --username=root --owner=root root

dropdb: 
	docker exec -it docker-postgres-1 dropdb alerts

migrateup:
	migrate -path db/migration \
		-database "cassandra://localhost:9042/acid_data?consistency=quorum" \
		-verbose up

migratedown:
	migrate -path db/migration \
		-database "cassandra://localhost:9042/acid_data?consistency=quorum" \
		-verbose down

# Create the keyspace first (ScyllaDB doesn't auto-create it)
create_keyspace:
	docker exec -it scylla-node1 cqlsh -e "CREATE KEYSPACE IF NOT EXISTS acid_data WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3};"

drop_keyspace:
	docker exec -it scylla-node1 cqlsh -e "DROP KEYSPACE IF EXISTS prod_data;"

# Run the main server (HTTP + gRPC)
run:
	go run cmd/api/main.go

# Test gRPC endpoints
test-grpc:
	go run cmd/grpc-client/main.go

# Generate proto files (if you modify acid.proto)
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/acid/acid.proto

	
.PHONY: create-secret postgres createdb dropdb migrateup migratedown sqlc test server mockdb delete-pods run test-grpc proto
