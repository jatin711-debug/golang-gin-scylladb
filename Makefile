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

	
.PHONY: create-secret postgres createdb dropdb migrateup migratedown sqlc test server mockdb delete-pods
