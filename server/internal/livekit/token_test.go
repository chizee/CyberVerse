package livekit

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken("test-key", "test-secret-that-is-long-enough-for-jwt", "room-123", "user-1", false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token")
	}

	// JWT should have 3 parts separated by dots
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("expected JWT with 3 parts, got %d", len(parts))
	}
}

func TestGenerateTokenDifferentIdentities(t *testing.T) {
	token1, _ := GenerateToken("key", "secret-that-is-long-enough-for-jwt", "room", "user-1", false)
	token2, _ := GenerateToken("key", "secret-that-is-long-enough-for-jwt", "room", "user-2", false)

	if token1 == token2 {
		t.Error("tokens for different identities should differ")
	}
}

func TestRoomName(t *testing.T) {
	name := RoomName("abc-123")
	if name != "cyberverse-abc-123" {
		t.Errorf("expected cyberverse-abc-123, got %s", name)
	}
}
