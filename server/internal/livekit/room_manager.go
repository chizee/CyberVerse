package livekit

import (
	"context"
	"fmt"

	lksdk "github.com/livekit/server-sdk-go/v2"
	lkproto "github.com/livekit/protocol/livekit"
)

// RoomManager handles LiveKit room lifecycle.
type RoomManager struct {
	client *lksdk.RoomServiceClient
	url        string
	apiKey     string
	apiSecret  string
}

// NewRoomManager creates a new room manager connected to the LiveKit server.
func NewRoomManager(host, apiKey, apiSecret string) *RoomManager {
	client := lksdk.NewRoomServiceClient(host, apiKey, apiSecret)
	return &RoomManager{
		client:     client,
		url:        host,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
	}
}

func (rm *RoomManager) URL() string {
	return rm.url
}

func (rm *RoomManager) APIKey() string {
	return rm.apiKey
}

func (rm *RoomManager) APISecret() string {
	return rm.apiSecret
}

// RoomName returns the standard room name for a session.
func RoomName(sessionID string) string {
	return "cyberverse-" + sessionID
}

// CreateRoom creates a new LiveKit room for a session.
func (rm *RoomManager) CreateRoom(ctx context.Context, roomName string) error {
	_, err := rm.client.CreateRoom(ctx, &lkproto.CreateRoomRequest{
		Name:            roomName,
		EmptyTimeout:    300, // 5 minutes
		MaxParticipants: 4,
	})
	if err != nil {
		return fmt.Errorf("failed to create room %s: %w", roomName, err)
	}
	return nil
}

// DeleteRoom deletes a LiveKit room.
func (rm *RoomManager) DeleteRoom(ctx context.Context, roomName string) error {
	_, err := rm.client.DeleteRoom(ctx, &lkproto.DeleteRoomRequest{
		Room: roomName,
	})
	if err != nil {
		return fmt.Errorf("failed to delete room %s: %w", roomName, err)
	}
	return nil
}
