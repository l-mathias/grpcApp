package main

import (
	"flag"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"grpcApp/pkg/client"
	"grpcApp/pkg/core"
	"grpcApp/proto"
	"log"
)

var (
	host, playerName, password string
)

func init() {
	flag.StringVar(&host, "h", "0.0.0.0:8080", "the pkg's host")
	flag.StringVar(&password, "p", "", "the pkg's password")
	flag.StringVar(&playerName, "n", "", "the username for the client")
	flag.Parse()
}

func main() {
	game := core.NewGame()
	game.IsAuthoritative = false
	game.Start()

	info := client.ConnectInfo{
		PlayerName: playerName,
		Address:    host,
		Password:   password,
	}

	conn, err := grpc.Dial(info.Address, grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		log.Fatalf("can not connect with pkg %v", err)
	}

	grpcClient := proto.NewGameClient(conn)
	client := client.NewGameClient(game)

	playerID := uuid.New()
	err = client.Login(grpcClient, playerID, info.PlayerName, info.Password)
	if err != nil {
		log.Fatalf("connect request failed %v", err)
	}
	client.Start()

}
