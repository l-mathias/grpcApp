package common

import (
	"fmt"
	"github.com/google/uuid"
	"log"
	"sync"
	"time"
)

const (
	debug            bool = true
	roundOverScore        = 10
	newRoundWaitTime      = 10 * time.Second
)

type Change interface{}

type RoundOverChange struct {
	Change
}

type RoundStartChange struct {
	Change
}

type AddPlayerChange struct {
	Change
	Player Player
}

type RemovePlayerChange struct {
	Change
	Player Player
}

type Action interface {
	Perform(game *Game)
}

type Message struct {
	From string
	Msg  string
}

type MessageAction struct {
	Message Message
	ID      uuid.UUID
	Created time.Time
}

type MessageChange struct {
	Change
	ID      uuid.UUID
	Message Message
	Created time.Time
}

type Player struct {
	UUID uuid.UUID
	Name string
}

type Game struct {
	// remove pointer for heap usage
	Players         map[uuid.UUID]*Player
	Mu              sync.RWMutex
	ChangeChannel   chan Change
	ActionChannel   chan Action
	lastAction      map[string]time.Time
	Score           map[uuid.UUID]int
	NewRoundAt      time.Time
	RoundWinner     uuid.UUID
	WaitForRound    bool
	IsAuthoritative bool
}

func NewGame() *Game {
	game := Game{
		Players:         make(map[uuid.UUID]*Player),
		ActionChannel:   make(chan Action, 1),
		lastAction:      make(map[string]time.Time),
		ChangeChannel:   make(chan Change, 1),
		IsAuthoritative: true,
		WaitForRound:    true,
		Score:           make(map[uuid.UUID]int),
	}
	return &game
}

func (game *Game) DisplayGame() {
	game.LogDebug(fmt.Sprintf("Displaying Game :\nNumber of player: %v\n", len(game.Players)))
	for _, p := range game.Players {
		game.LogDebug(fmt.Sprintf("Player: %v\n", p.Name))
	}
}

func (game *Game) Start() {
	go game.watchActions()
}

func (game *Game) watchActions() {
	for {
		action := <-game.ActionChannel
		if game.WaitForRound {
			continue
		}
		game.Mu.Lock()
		action.Perform(game)
		game.Mu.Unlock()
	}
}

func (p *Player) ID() uuid.UUID {
	return p.UUID
}

func (game *Game) GetPlayer(id uuid.UUID) *Player {
	return game.Players[id]
}

func (game *Game) PlayerExists(id uuid.UUID) bool {
	if _, ok := game.Players[id]; ok {
		return true
	}
	return false
}

func (game *Game) AddPlayer(player *Player) {
	game.Players[player.ID()] = player
}

func (game *Game) RemovePlayer(id uuid.UUID) {
	delete(game.Players, id)
}

func (message MessageAction) Perform(game *Game) {
	change := MessageChange{
		Message: message.Message,
	}
	game.sendChange(change)
}

func (game *Game) sendChange(change Change) {
	select {
	case game.ChangeChannel <- change:
	default:
	}
}

func (game *Game) LogDebug(msg string) {
	if debug {
		log.Printf(msg)
	}
}

func (game *Game) queueNewRound(roundWinner uuid.UUID) {
	game.WaitForRound = true
	game.NewRoundAt = time.Now().Add(newRoundWaitTime)
	game.RoundWinner = roundWinner
	game.sendChange(RoundOverChange{})
	go func() {
		time.Sleep(newRoundWaitTime)
		game.Mu.Lock()
		game.StartNewRound()
		game.Mu.Unlock()
	}()
}

func (game *Game) StartNewRound() {
	game.WaitForRound = false
	game.Score = map[uuid.UUID]int{}
	game.sendChange(RoundStartChange{})
}

func (game *Game) AddScore(id uuid.UUID) {
	game.Score[id]++
}
