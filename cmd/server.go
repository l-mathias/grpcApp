package main

import (
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"grpcApp/pkg/core"
	"grpcApp/pkg/server"
	"grpcApp/proto"
	"log"
	"net"
)

func main() {
	port := flag.Int("port", 8080, "The port to listen on.")
	password := flag.String("password", "", "The pkg password.")
	flag.Parse()

	log.Printf("listening on port %d\n", *port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	game := core.NewGame()

	game.Start()

	s := grpc.NewServer()
	server := server.NewGameServer(game, *password)
	proto.RegisterGameServer(s, server)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
