package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"grpcApp/pkg/core"
	"grpcApp/proto"
	"log"
	"regexp"
	"sync"
	"time"
)

const (
	clientTimeout = 15
	maxClients    = 2
)

type client struct {
	streamServer proto.Game_StreamServer
	lastMessage  time.Time
	done         chan error
	playerID     uuid.UUID
	id           uuid.UUID
	active       bool
}

type GameServer struct {
	proto.UnimplementedGameServer
	game     *core.Game
	clients  map[uuid.UUID]*client
	mu       sync.RWMutex
	password string
}

func NewGameServer(game *core.Game, password string) *GameServer {
	server := &GameServer{
		game:     game,
		clients:  make(map[uuid.UUID]*client),
		password: password,
	}
	server.watchChanges()
	server.watchTimeout()
	server.watchPlay()
	return server
}

func (s *GameServer) watchPlay() {
	go func() {
		for {
			if len(s.clients) == 2 && s.game.WaitForRound {
				log.Printf("s.client len : %v - waitForRound : %v", len(s.clients), s.game.WaitForRound)
				s.game.StartNewRound()

				resp := proto.StreamResponse{
					Event: &proto.StreamResponse_ResponseMessage{
						ResponseMessage: &proto.Message{
							From:    "Server",
							Message: "We have 2 players... Let's begin !",
						},
					},
				}
				s.broadcast(&resp)
				s.invertActive()
			}
		}
	}()
}

func (s *GameServer) watchTimeout() {
	timeoutTicker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			for _, client := range s.clients {
				if time.Now().Sub(client.lastMessage).Minutes() > clientTimeout {
					client.done <- errors.New("you have been timed out")
					return
				}
			}
			<-timeoutTicker.C
		}
	}()
}

func (s *GameServer) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	if len(s.clients) >= maxClients {
		return nil, errors.New("The pkg is full")
	}

	playerID, err := uuid.Parse(req.Id)
	if err != nil {
		return nil, err
	}

	// Exit as early as possible if password is wrong.
	if req.Password != s.password {
		return nil, errors.New("invalid password provided")
	}

	// Check if player already exists.
	if s.game.PlayerExists(playerID) {
		return nil, errors.New("duplicate player ID provided")
	}

	re := regexp.MustCompile("^[a-zA-Z0-9]+$")
	if !re.MatchString(req.Name) {
		return nil, errors.New("invalid name provided")
	}

	// Add the player.
	player := &core.Player{
		Name: req.Name,
		UUID: playerID,
	}

	s.game.AddPlayer(player)

	s.game.LogDebug(fmt.Sprintf("Player added : \nUUID: %v\nName: %v\n", player.UUID, player.Name))

	// Build a slice of current entities.
	s.game.Mu.Lock()
	players := make([]*proto.Player, 0)
	for _, player := range s.game.Players {
		protoPlayer := proto.GetProtoPlayer(&player)
		if protoPlayer != nil {
			players = append(players, protoPlayer)
		}
	}
	s.game.Mu.Unlock()

	// Inform all other clients of the new player.
	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_AddPlayer{
			AddPlayer: &proto.AddPlayer{
				Player: proto.GetProtoPlayer(player),
			},
		},
	}
	s.broadcast(&resp)

	// Add the new client.
	s.mu.Lock()
	token := uuid.New()
	s.clients[token] = &client{
		id:          token,
		playerID:    playerID,
		done:        make(chan error),
		lastMessage: time.Now(),
		active:      len(s.clients) == 0,
	}
	s.mu.Unlock()

	return &proto.LoginResponse{
		Token:   token.String(),
		Players: players,
	}, nil
}

func (s *GameServer) displayClients() {
	s.mu.Lock()
	s.game.LogDebug(fmt.Sprintf("Clients :%v\n", len(s.clients)))
	for _, c := range s.clients {
		s.game.LogDebug(fmt.Sprintf("\nclientID: %v\nclientPlayerID: %v\nclientActive: %v\nplayerName: %v\nclientStream: %v\n-------------\n", c.id, c.playerID, c.active, s.game.GetPlayer(c.playerID).Name, c.streamServer))
	}
	s.mu.Unlock()
}

func (s *GameServer) send(from, to, msg string) {
	specMsg := proto.StreamResponse{
		Event: &proto.StreamResponse_ResponseMessage{
			ResponseMessage: &proto.Message{
				From:    from,
				To:      to,
				Message: msg,
			},
		},
	}

	s.mu.Lock()

	dest, err := uuid.Parse(to)

	if err != nil {
		log.Printf("bad UUID %v\n", err)
	}

	if err = s.clients[dest].streamServer.Send(&specMsg); err != nil {
		log.Printf("%s - sending error %v", to, err)
		s.clients[dest].done <- errors.New("failed to send message")
	}
	s.mu.Unlock()
}

func (s *GameServer) broadcast(resp *proto.StreamResponse) {
	s.mu.Lock()
	for id, currentClient := range s.clients {
		if currentClient.streamServer == nil {
			continue
		}
		if err := currentClient.streamServer.Send(resp); err != nil {
			log.Printf("%s - broadcast error %v", id, err)
			currentClient.done <- errors.New("failed to broadcast message")
			continue
		}
		log.Printf("%s - broadcasted %+v", resp, id)
	}
	s.mu.Unlock()
}

func (s *GameServer) handleRoundOverChange(change core.RoundOverChange) {
	s.game.Mu.RLock()
	defer s.game.Mu.RUnlock()
	timestamp, err := ptypes.TimestampProto(s.game.NewRoundAt)
	if err != nil {
		log.Fatalf("unable to parse new round timestamp %v", s.game.NewRoundAt)
	}
	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_RoundOver{
			RoundOver: &proto.RoundOver{
				RoundWinnerId: s.game.RoundWinner.String(),
				NewRoundAt:    timestamp,
			},
		},
	}
	s.broadcast(&resp)
}

func (s *GameServer) handleRoundStartChange(change core.RoundStartChange) {
	players := []*proto.Player{}
	s.game.Mu.Lock()
	for _, player := range s.game.Players {
		players = append(players, proto.GetProtoPlayer(&player))
	}
	s.game.Mu.Unlock()
	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_RoundStart{
			RoundStart: &proto.RoundStart{
				Players: players,
			},
		},
	}
	s.broadcast(&resp)
}

func (s *GameServer) handleAddPlayerChange(change core.AddPlayerChange) {
	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_AddPlayer{
			AddPlayer: &proto.AddPlayer{
				Player: proto.GetProtoPlayer(&change.Player),
			},
		},
	}
	s.broadcast(&resp)
}

func (s *GameServer) handleRemovePlayerChange(change core.RemovePlayerChange) {
	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_RemovePlayer{
			RemovePlayer: &proto.RemovePlayer{
				Id: change.Player.ID().String(),
			},
		},
	}
	s.broadcast(&resp)
}

func (s *GameServer) watchChanges() {
	go func() {
		for change := range s.game.ChangeChannel {
			switch change.(type) {
			case core.AddPlayerChange:
				log.Printf("Received AddPlayerChange\n")
				change := change.(core.AddPlayerChange)
				s.handleAddPlayerChange(change)
			case core.RemovePlayerChange:
				log.Printf("Received RemovePlayerChange\n")
				change := change.(core.RemovePlayerChange)
				s.handleRemovePlayerChange(change)
			case core.RoundOverChange:
				log.Printf("Received RoundOverChange\n")
				change := change.(core.RoundOverChange)
				s.handleRoundOverChange(change)
			case core.RoundStartChange:
				log.Printf("Received RoundStartChange\n")
				change := change.(core.RoundStartChange)
				s.handleRoundStartChange(change)
			}
		}
	}()
}

func (s *GameServer) getClientFromContext(ctx context.Context) (*client, error) {
	headers, ok := metadata.FromIncomingContext(ctx)
	tokenRaw := headers["authorization"]
	if len(tokenRaw) == 0 {
		return nil, errors.New("no token provided")
	}
	token, err := uuid.Parse(tokenRaw[0])
	if err != nil {
		return nil, errors.New("cannot parse token")
	}
	s.mu.RLock()
	currentClient, ok := s.clients[token]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("token not recognized")
	}
	return currentClient, nil
}

func (s *GameServer) Stream(srv proto.Game_StreamServer) error {
	ctx := srv.Context()
	currentClient, err := s.getClientFromContext(ctx)
	if err != nil {
		return err
	}
	if currentClient.streamServer != nil {
		return errors.New("stream already active")
	}
	currentClient.streamServer = srv

	// Wait for stream requests.
	go func() {
		for {
			req, err := srv.Recv()
			if err != nil {
				log.Printf("receive error %v", err)
				currentClient.done <- errors.New("failed to receive request")
				return
			}
			currentClient.lastMessage = time.Now()

			s.displayClients()
			log.Printf("got StreamRequest_RequestMessage %+v", req)
			s.handleMessageRequest(req, currentClient)
		}
	}()

	// Wait for stream to be done.
	var doneError error
	select {
	case <-ctx.Done():
		doneError = ctx.Err()
	case doneError = <-currentClient.done:
	}
	log.Printf(`stream done with error "%v"`, doneError)

	log.Printf("%s - removing client", currentClient.id)
	s.removeClient(currentClient.id)
	s.removePlayer(currentClient.playerID)

	s.mu.Lock()
	for _, c := range s.clients {
		if c.playerID != currentClient.playerID {
			log.Printf("setting %v active", s.game.Players[c.playerID].Name)
			c.active = true
		}
	}
	s.mu.Unlock()

	// this client is not the winner in case of disconnect, change this
	s.game.QueueNewRound(currentClient.playerID)
	return doneError
}

func (s *GameServer) invertActive() {
	for _, c := range s.clients {
		c.active = !c.active
		if c.active {
			s.send("Server", c.id.String(), "This is your turn.")
		} else {
			s.send("Server", c.id.String(), "Waiting for opponent...")
		}
	}
}

func (s *GameServer) handleMessageRequest(req *proto.StreamRequest, currentClient *client) {

	if currentClient.active {
		msg := req.GetRequestMessage()

		resp := proto.StreamResponse{
			Event: &proto.StreamResponse_ResponseMessage{
				ResponseMessage: &proto.Message{
					From:    msg.From,
					Message: msg.Message,
				},
			},
		}
		s.broadcast(&resp)
		if len(s.clients) == 2 {
			s.invertActive()
		}
	} else {
		s.send("Server", currentClient.id.String(), "Not your turn !")
	}
}

func (s *GameServer) removeClient(id uuid.UUID) {
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()
}

func (s *GameServer) removePlayer(playerID uuid.UUID) {
	s.game.RemovePlayer(playerID)

	resp := proto.StreamResponse{
		Event: &proto.StreamResponse_RemovePlayer{
			RemovePlayer: &proto.RemovePlayer{
				Id: playerID.String(),
			},
		},
	}
	s.broadcast(&resp)
}
