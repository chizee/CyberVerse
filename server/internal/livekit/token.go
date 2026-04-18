package livekit

import (
	"time"

	"github.com/livekit/protocol/auth"
)

// GenerateToken creates a LiveKit JWT token for a participant to join a room.
func GenerateToken(apiKey, apiSecret, roomName, identity string, canPublish bool) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	canPublishPtr := new(bool)
	*canPublishPtr = canPublish
	canSubscribePtr := new(bool)
	*canSubscribePtr = true

	grant := &auth.VideoGrant{
		RoomJoin: true,
		Room:     roomName,
		CanPublish: canPublishPtr,
		CanSubscribe: canSubscribePtr,
	}

	at.SetVideoGrant(grant).
		SetIdentity(identity).
		SetValidFor(24 * time.Hour)

	return at.ToJWT()
}
