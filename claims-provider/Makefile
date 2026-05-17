build:
	go build .
	go build -o claims-client ./client

protoc:
	protoc proto/claims-provider.proto --go_out=. --go_opt=paths=source_relative \
		   --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/claims-provider.proto

run:
	./claims-provider s