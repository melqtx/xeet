package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/melqtx/xeet/pkg/config"
)

func TestEncodeTransactionIDShape(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	id, err := encodeTransactionID("POST", "/i/api/graphql/id/CreateTweet",
		time.Unix(transactionEpoch+12345, 0), key, "animation")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1+len(key)+4+16+1 {
		t.Fatalf("decoded transaction length=%d", len(decoded))
	}
}

func TestTransactionIDLive(t *testing.T) {
	if os.Getenv("XEET_LIVE_TRANSACTION") == "" {
		t.Skip("set XEET_LIVE_TRANSACTION=1 to parse X's current transaction state")
	}
	manager, err := config.NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	client := NewWebClient(cfg)
	key, animation, err := loadTransactionState(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	keyHash := sha256.Sum256(key)
	animationHash := sha256.Sum256([]byte(animation))
	t.Logf("key=%s animation=%s animation_length=%d", hex.EncodeToString(keyHash[:8]), hex.EncodeToString(animationHash[:8]), len(animation))
	id, err := encodeTransactionID("POST", "/i/api/graphql/test/CreateTweet", time.Now(), key, animation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(id, "=") || len(id) < 60 {
		t.Fatalf("invalid generated transaction id shape (length %d)", len(id))
	}
}
