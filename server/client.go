package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"grpcApp/proto"
	"grpcApp/server/common"
	"log"
	"os"
	"time"
)

type GameClient struct {
	CurrentPlayer uuid.UUID
	Stream        proto.Game_StreamClient
	Game          *common.Game
}

type connectInfo struct {
	PlayerName string
	Address    string
	Password   string
}

var (
	host, playerName, password string
)

func init() {
	flag.StringVar(&host, "h", "0.0.0.0:8080", "the server's host")
	flag.StringVar(&password, "p", "", "the server's password")
	flag.StringVar(&playerName, "n", "", "the username for the client")
	flag.Parse()
}

func NewGameClient(game *common.Game) *GameClient {
	return &GameClient{
		Game: game,
	}
}

func (c *GameClient) Login(grpcClient proto.GameClient, playerID uuid.UUID, playerName string, password string) error {
	// Connect to server.
	req := proto.LoginRequest{
		Id:       playerID.String(),
		Name:     playerName,
		Password: password,
	}
	resp, err := grpcClient.Login(context.Background(), &req)
	if err != nil {
		return err
	}

	// Add initial entity state.
	for _, player := range resp.Players {
		commonPlayer := proto.GetCommonPlayer(player)
		if commonPlayer == nil {
			return fmt.Errorf("can not get backend entity from %+v", player)
		}
		c.Game.AddPlayer(commonPlayer)
	}

	// Initialize stream with token.
	header := metadata.New(map[string]string{"authorization": resp.Token})
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := grpcClient.Stream(ctx)
	if err != nil {
		return err
	}

	c.CurrentPlayer = playerID
	c.Stream = stream
	c.Game.LogDebug(fmt.Sprintf("New client added %v\n", c.CurrentPlayer))

	return nil
}

func (c *GameClient) Exit(message string) {
	log.Println(message)
}

func (c *GameClient) handleMessageChange(change common.MessageChange) {

	req := proto.StreamRequest{
		RequestMessage: &proto.Message{
			From:    change.Player.Name,
			Message: change.Message.Msg,
		},
	}
	c.Stream.Send(&req)
}

func (c *GameClient) Start() {
	// Handle local game engine changes.
	go func() {
		for {
			change := <-c.Game.ChangeChannel
			switch change.(type) {
			case common.MessageChange:
				c.Game.LogDebug(fmt.Sprintf("Received MessageChange\n"))
				change := change.(common.MessageChange)
				c.handleMessageChange(change)
			}
		}
	}()
	// Handle stream messages.
	go func() {
		for {
			resp, err := c.Stream.Recv()
			if err != nil {
				c.Exit(fmt.Sprintf("can not receive, error: %v", err))
				return
			}

			c.Game.Mu.Lock()
			switch resp.GetEvent().(type) {
			case *proto.StreamResponse_RemovePlayer:
				c.Game.LogDebug(fmt.Sprintf("Received StreamResponse_RemovePlayer\n"))
				c.handleClientRemovePlayer(resp)
			case *proto.StreamResponse_AddPlayer:
				c.Game.LogDebug(fmt.Sprintf("Received StreamResponse_AddPlayer\n"))
				c.handleClientAddPlayer(resp)
			case *proto.StreamResponse_ResponseMessage:
				c.Game.LogDebug(fmt.Sprintf("Received StreamResponse_ClientMessage\n"))
				c.handleClientMessageResponse(resp)
			}
			c.Game.Mu.Unlock()
		}
	}()

	sc := bufio.NewScanner(os.Stdin)
	sc.Split(bufio.ScanLines)
	for {
		if sc.Scan() {
			c.Game.DisplayGame()

			c.Game.ActionChannel <- common.MessageAction{
				ID: c.CurrentPlayer,
				Message: common.Message{
					From: c.Game.GetPlayer(c.CurrentPlayer).Name,
					Msg:  sc.Text(),
				},
				Created: time.Now(),
			}
		} else {
			log.Printf("input scanner failure: %v", sc.Err())
			return
		}
	}
}

func (c *GameClient) handleClientRemovePlayer(resp *proto.StreamResponse) {
	e := resp.GetRemovePlayer()
	id, _ := uuid.Parse(e.Id)
	log.Printf("%v left the game\n", c.Game.GetPlayer(id).Name)
	c.Game.RemovePlayer(id)
}

func (c *GameClient) handleClientAddPlayer(resp *proto.StreamResponse) {
	e := resp.GetAddPlayer()
	c.Game.AddPlayer(proto.GetCommonPlayer(e.Player))
	log.Printf("%v joined the game\n", e.Player.Name)
}

func (c *GameClient) handleClientMessageResponse(resp *proto.StreamResponse) {
	e := resp.GetResponseMessage()
	log.Printf("%v > %v\n", e.From, e.Message)
}

func main() {

	game := common.NewGame()
	game.IsAuthoritative = false
	game.Start()

	info := connectInfo{
		PlayerName: playerName,
		Address:    host,
		Password:   password,
	}

	conn, err := grpc.Dial(info.Address, grpc.WithInsecure())
	defer conn.Close()
	if err != nil {
		log.Fatalf("can not connect with server %v", err)
	}

	grpcClient := proto.NewGameClient(conn)
	client := NewGameClient(game)

	playerID := uuid.New()
	err = client.Login(grpcClient, playerID, info.PlayerName, info.Password)
	if err != nil {
		log.Fatalf("connect request failed %v", err)
	}
	client.Start()

}
