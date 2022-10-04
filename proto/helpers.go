package proto

import (
	"github.com/google/uuid"
	"grpcApp/pkg/core"
	"log"
)

func GetProtoPlayer(player *core.Player) *Player {
	return &Player{
		Id:   player.ID().String(),
		Name: player.Name,
	}
}

func GetCommonPlayer(protoPlayer *Player) *core.Player {
	playerId, err := uuid.Parse(protoPlayer.Id)
	if err != nil {
		log.Printf("failed to convert proto UUID: %+v", err)
		return nil
	}

	player := &core.Player{
		UUID: playerId,
		Name: protoPlayer.Name,
	}
	return player
}
