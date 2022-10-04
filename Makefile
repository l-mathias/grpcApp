PROJECT=grpcApp
VERSION=$$(git rev-parse --short=10 HEAD)

proto:
	protoc --go_out=.\proto --go-grpc_out=.\proto proto/*.proto

clean:
	go clean -cache

build_client:
	go build cmd\client.go

build_server:
	go build cmd\server.go

build_all:
	go build cmd\server.go && go build cmd\client.go

run_server:
	go run cmd\server.go
