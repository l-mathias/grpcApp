package client

import (
	"bufio"
	"context"
	"fmt"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"grpcApp/pkg/core"
	"grpcApp/proto"
	"log"
	"os"
	"time"
)

type GameClient struct {
	CurrentPlayer uuid.UUID
	Stream        proto.Game_StreamClient
	Game          *core.Game
}

type ConnectInfo struct {
	PlayerName string
	Address    string
	Password   string
}

func NewGameClient(game *core.Game) *GameClient {
	return &GameClient{
		Game: game,
	}
}

func (c *GameClient) Login(grpcClient proto.GameClient, playerID uuid.UUID, playerName string, password string) error {
	// Connect to pkg.
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

func (c *GameClient) handleMessageChange(change core.MessageChange) {

	req := proto.StreamRequest{
		RequestMessage: &proto.Message{
			From:    change.Message.From,
			Message: change.Message.Msg,
		},
	}
	err := c.Stream.Send(&req)
	if err != nil {
		log.Printf("Error sending : %v", err)
	}
}

func (c *GameClient) Start() {
	// Handle local game engine changes.
	go func() {
		for change := range c.Game.ChangeChannel {
			switch change.(type) {
			case core.MessageChange:
				c.Game.LogDebug(fmt.Sprintf("Received MessageChange\n"))
				change := change.(core.MessageChange)
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
			case *proto.StreamResponse_RoundOver:
				//c.Game.LogDebug(fmt.Sprintf("Received StreamResponse_RoundOver\n"))
				log.Printf("received RoundOver Response")
				c.handleRoundOver(resp)
			}
			c.Game.Mu.Unlock()
		}
	}()

	sc := bufio.NewScanner(os.Stdin)
	sc.Split(bufio.ScanLines)
	for {
		if sc.Scan() {
			c.Game.DisplayGame()

			c.Game.ChangeChannel <- core.MessageChange{
				ID: c.CurrentPlayer,
				Message: core.Message{
					From: c.Game.GetPlayer(c.CurrentPlayer).Name,
					Msg:  sc.Text(),
				},
				Created: time.Now(),
			}
		} else {
			log.Fatalf("input scanner failure: %v", sc.Err())
			return
		}
	}
}

func (c *GameClient) handleRoundOver(resp *proto.StreamResponse) {
	log.Printf("handled RoundOverChange")
}

func (c *GameClient) handleClientRemovePlayer(resp *proto.StreamResponse) {
	e := resp.GetRemovePlayer()
	id, _ := uuid.Parse(e.Id)
	log.Printf("%v left the game\n", c.Game.GetPlayer(id).Name)
	c.Game.RemovePlayer(id)
}

func (c *GameClient) handleClientAddPlayer(resp *proto.StreamResponse) {
	e := resp.GetAddPlayer()

	//c.Game.Mu.Lock()
	c.Game.AddPlayer(proto.GetCommonPlayer(e.Player))
	//c.Game.Mu.Unlock()

	log.Printf("%v joined the game\n", e.Player.Name)
}

func (c *GameClient) handleClientMessageResponse(resp *proto.StreamResponse) {
	e := resp.GetResponseMessage()
	log.Printf("%v > %v\n", e.From, e.Message)
}
