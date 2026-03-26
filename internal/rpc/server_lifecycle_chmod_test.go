package rpc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	sqlitestorage "github.com/steveyegge/beads/internal/storage/sqlite"
)

func TestStart_ChmodFailureIsNonFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "bd-rpc-chmod-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if !strings.Contains(tmpDir, os.TempDir()) {
		t.Fatalf("tmpDir must be in system temp directory, got: %s", tmpDir)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	dbPath := filepath.Join(beadsDir, "test.db")
	socketPath := filepath.Join(beadsDir, "bd-chmod-test.sock")
	os.Remove(socketPath)

	store, err := sqlitestorage.New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	// Override chmod to simulate Docker Desktop fakeowner EINVAL
	origChmod := chmodFunc
	chmodFunc = func(name string, mode os.FileMode) error {
		if name == socketPath {
			return &os.PathError{Op: "chmod", Path: name, Err: syscall.EINVAL}
		}
		return origChmod(name, mode)
	}
	defer func() { chmodFunc = origChmod }()

	server := NewServer(socketPath, store, tmpDir, dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(ctx)
	}()

	// Server should become ready despite chmod failure
	select {
	case <-server.WaitReady():
		// Success: server started despite chmod error
	case err := <-serverErr:
		t.Fatalf("Server failed to start (chmod failure was fatal): %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not become ready within timeout")
	}
	defer server.Stop()

	// Verify server is functional — can accept connections
	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("Failed to connect after chmod failure: %v", err)
	}
	defer client.Close()

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping failed after chmod failure: %v", err)
	}
}
