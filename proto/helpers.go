package proto

import (
	"github.com/google/uuid"
	"grpcApp/server/common"
	"log"
)

func GetProtoPlayer(player *common.Player) *Player {
	return &Player{
		Id:   player.ID().String(),
		Name: player.Name,
	}
}

func GetCommonPlayer(protoPlayer *Player) *common.Player {
	playerId, err := uuid.Parse(protoPlayer.Id)
	if err != nil {
		log.Printf("failed to convert proto UUID: %+v", err)
		return nil
	}

	player := &common.Player{
		UUID: playerId,
		Name: protoPlayer.Name,
	}
	return player
}
