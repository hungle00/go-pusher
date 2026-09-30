package app

import (
	"bytes"
	"crypto/sha256"
	"path/filepath"
	"testing"
)

func TestRegistryPersistsAppsWithoutStoringSecret(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "pusher.db")
	registry, err := OpenRegistry(databasePath)
	if err != nil {
		t.Fatal(err)
	}

	created, err := registry.Create("Example")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}

	registry, err = OpenRegistry(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	keyValid, err := registry.HasKey(created.ID, created.Key)
	if err != nil || !keyValid {
		t.Fatalf("expected persisted public key to be valid; valid=%v err=%v", keyValid, err)
	}
	secretValid, err := registry.HasSecret(created.ID, created.Secret)
	if err != nil || !secretValid {
		t.Fatalf("expected persisted secret to be valid; valid=%v err=%v", secretValid, err)
	}

	var storedHash []byte
	if err := registry.db.QueryRow(`SELECT secret_hash FROM apps WHERE id = ?`, created.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(created.Secret))
	if !bytes.Equal(storedHash, wantHash[:]) {
		t.Fatal("database should contain the secret hash, not the raw secret")
	}
}
