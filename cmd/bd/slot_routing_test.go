package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestSlotShowWithRouting tests that bd slot show respects routes.jsonl
// for cross-repo agent resolution. This is a regression test for the bug where
// bd slot commands failed to find agents in routed databases.
//
// NOTE: This test uses os.Chdir and cannot run in parallel with other tests.
func TestSlotShowWithRouting(t *testing.T) {
	ctx := context.Background()

	// Create temp directory structure:
	// tmpDir/
	//   .beads/
	//     beads.db (town database)
	//     routes.jsonl (routing config)
	//   rig/
	//     .beads/
	//       beads.db (rig database with agent)
	tmpDir := t.TempDir()

	// Create town .beads directory
	townBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create town beads dir: %v", err)
	}

	// Create rig .beads directory
	rigBeadsDir := filepath.Join(tmpDir, "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create rig beads dir: %v", err)
	}

	// Initialize town database using helper (prefix without trailing hyphen)
	townDBPath := filepath.Join(townBeadsDir, "beads.db")
	townStore := newTestStoreWithPrefix(t, townDBPath, "hq")

	// Initialize rig database using helper (prefix without trailing hyphen)
	rigDBPath := filepath.Join(rigBeadsDir, "beads.db")
	rigStore := newTestStoreWithPrefix(t, rigDBPath, "gt")

	// Create an agent bead in the rig database
	// Note: slot commands check IssueType == "agent", so we use that string
	agentBead := &types.Issue{
		ID:        "gt-testrig-polecat-slot",
		Title:     "Agent: gt-testrig-polecat-slot",
		IssueType: types.IssueType("agent"),
		Status:    types.StatusOpen,
		RoleType:  "polecat",
		Rig:       "testrig",
	}
	if err := rigStore.CreateIssue(ctx, agentBead, "test"); err != nil {
		t.Fatalf("Failed to create agent bead: %v", err)
	}
	// Set hook_bead via UpdateIssue (CreateIssue doesn't include agent fields)
	if err := rigStore.UpdateIssue(ctx, agentBead.ID, map[string]interface{}{
		"hook_bead": "hq-work123",
		"role_bead": "gt-role-polecat",
	}, "test"); err != nil {
		t.Fatalf("Failed to set agent slots: %v", err)
	}

	// Create routes.jsonl in town .beads directory
	routesContent := `{"prefix":"gt-","path":"rig"}`
	routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatalf("Failed to write routes.jsonl: %v", err)
	}

	// Set up global state for routing to work
	oldDbPath := dbPath
	dbPath = townDBPath
	t.Cleanup(func() { dbPath = oldDbPath })

	// Change to tmpDir so routing can find town root via CWD
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// Test: resolve agent via routing (simulates what slot show should do)
	// The fix will add routing support to slot show
	result, err := resolveAgentWithRouting(ctx, townStore, "gt-testrig-polecat-slot")
	if err != nil {
		t.Fatalf("resolveAgentWithRouting failed: %v", err)
	}
	if result == nil {
		t.Fatal("resolveAgentWithRouting returned nil result")
	}
	defer result.Close()

	if result.Issue == nil {
		t.Fatal("resolveAgentWithRouting returned nil issue")
	}

	if result.Issue.ID != "gt-testrig-polecat-slot" {
		t.Errorf("Expected issue ID %q, got %q", "gt-testrig-polecat-slot", result.Issue.ID)
	}

	if !result.Routed {
		t.Error("Expected result.Routed to be true for cross-repo lookup")
	}

	// Verify we can read the slot values
	if result.Issue.HookBead != "hq-work123" {
		t.Errorf("Expected HookBead %q, got %q", "hq-work123", result.Issue.HookBead)
	}

	t.Logf("Successfully resolved agent %s via routing for slot show", result.Issue.ID)
}

// TestSlotSetWithRouting tests that bd slot set respects routes.jsonl
// for cross-repo agent resolution.
//
// NOTE: This test uses os.Chdir and cannot run in parallel with other tests.
func TestSlotSetWithRouting(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create directory structure
	townBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create town beads dir: %v", err)
	}

	rigBeadsDir := filepath.Join(tmpDir, "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create rig beads dir: %v", err)
	}

	// Initialize databases
	townDBPath := filepath.Join(townBeadsDir, "beads.db")
	townStore := newTestStoreWithPrefix(t, townDBPath, "hq")

	rigDBPath := filepath.Join(rigBeadsDir, "beads.db")
	rigStore := newTestStoreWithPrefix(t, rigDBPath, "gt")

	// Create an agent bead in the rig database (with empty hook)
	// Note: slot commands check IssueType == "agent", so we use that string
	agentBead := &types.Issue{
		ID:        "gt-testrig-polecat-settest",
		Title:     "Agent: gt-testrig-polecat-settest",
		IssueType: types.IssueType("agent"),
		Status:    types.StatusOpen,
		RoleType:  "polecat",
		Rig:       "testrig",
		HookBead:  "", // Empty hook for set test
	}
	if err := rigStore.CreateIssue(ctx, agentBead, "test"); err != nil {
		t.Fatalf("Failed to create agent bead: %v", err)
	}

	// Create a work bead in the town database
	workBead := &types.Issue{
		ID:        "hq-work456",
		Title:     "Test work item",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
	}
	if err := townStore.CreateIssue(ctx, workBead, "test"); err != nil {
		t.Fatalf("Failed to create work bead: %v", err)
	}

	// Create routes.jsonl
	routesContent := `{"prefix":"gt-","path":"rig"}`
	routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatalf("Failed to write routes.jsonl: %v", err)
	}

	// Set up global state
	oldDbPath := dbPath
	dbPath = townDBPath
	t.Cleanup(func() { dbPath = oldDbPath })

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// Test: resolve agent via routing and update the slot
	result, err := resolveAgentWithRouting(ctx, townStore, "gt-testrig-polecat-settest")
	if err != nil {
		t.Fatalf("resolveAgentWithRouting failed: %v", err)
	}
	if result == nil || result.Issue == nil {
		t.Fatal("resolveAgentWithRouting returned nil")
	}
	defer result.Close()

	// Verify the agent was found via routing
	if !result.Routed {
		t.Error("Expected result.Routed to be true")
	}

	// Now update the slot using the routed store
	updates := map[string]interface{}{
		"hook_bead": "hq-work456",
	}
	if err := result.Store.UpdateIssue(ctx, result.ResolvedID, updates, "test"); err != nil {
		t.Fatalf("Failed to update slot: %v", err)
	}

	// Verify the update persisted
	updatedAgent, err := rigStore.GetIssue(ctx, "gt-testrig-polecat-settest")
	if err != nil {
		t.Fatalf("Failed to get updated agent: %v", err)
	}
	if updatedAgent.HookBead != "hq-work456" {
		t.Errorf("Expected HookBead %q, got %q", "hq-work456", updatedAgent.HookBead)
	}

	t.Logf("Successfully set slot on agent %s via routing", result.Issue.ID)
}

// TestSlotClearWithRouting tests that bd slot clear respects routes.jsonl
// for cross-repo agent resolution.
//
// NOTE: This test uses os.Chdir and cannot run in parallel with other tests.
func TestSlotClearWithRouting(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create directory structure
	townBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create town beads dir: %v", err)
	}

	rigBeadsDir := filepath.Join(tmpDir, "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create rig beads dir: %v", err)
	}

	// Initialize databases
	townDBPath := filepath.Join(townBeadsDir, "beads.db")
	townStore := newTestStoreWithPrefix(t, townDBPath, "hq")

	rigDBPath := filepath.Join(rigBeadsDir, "beads.db")
	rigStore := newTestStoreWithPrefix(t, rigDBPath, "gt")

	// Create an agent bead in the rig database (with hook set)
	// Note: slot commands check IssueType == "agent", so we use that string
	agentBead := &types.Issue{
		ID:        "gt-testrig-polecat-cleartest",
		Title:     "Agent: gt-testrig-polecat-cleartest",
		IssueType: types.IssueType("agent"),
		Status:    types.StatusOpen,
		RoleType:  "polecat",
		Rig:       "testrig",
	}
	if err := rigStore.CreateIssue(ctx, agentBead, "test"); err != nil {
		t.Fatalf("Failed to create agent bead: %v", err)
	}
	// Set hook_bead via UpdateIssue (CreateIssue doesn't include agent fields)
	if err := rigStore.UpdateIssue(ctx, agentBead.ID, map[string]interface{}{
		"hook_bead": "hq-work789",
	}, "test"); err != nil {
		t.Fatalf("Failed to set agent hook: %v", err)
	}

	// Create routes.jsonl
	routesContent := `{"prefix":"gt-","path":"rig"}`
	routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatalf("Failed to write routes.jsonl: %v", err)
	}

	// Set up global state
	oldDbPath := dbPath
	dbPath = townDBPath
	t.Cleanup(func() { dbPath = oldDbPath })

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// Test: resolve agent via routing and clear the slot
	result, err := resolveAgentWithRouting(ctx, townStore, "gt-testrig-polecat-cleartest")
	if err != nil {
		t.Fatalf("resolveAgentWithRouting failed: %v", err)
	}
	if result == nil || result.Issue == nil {
		t.Fatal("resolveAgentWithRouting returned nil")
	}
	defer result.Close()

	// Verify the agent was found via routing
	if !result.Routed {
		t.Error("Expected result.Routed to be true")
	}

	// Verify hook is initially set
	if result.Issue.HookBead != "hq-work789" {
		t.Errorf("Expected initial HookBead %q, got %q", "hq-work789", result.Issue.HookBead)
	}

	// Now clear the slot using the routed store
	updates := map[string]interface{}{
		"hook_bead": "",
	}
	if err := result.Store.UpdateIssue(ctx, result.ResolvedID, updates, "test"); err != nil {
		t.Fatalf("Failed to clear slot: %v", err)
	}

	// Verify the update persisted
	updatedAgent, err := rigStore.GetIssue(ctx, "gt-testrig-polecat-cleartest")
	if err != nil {
		t.Fatalf("Failed to get updated agent: %v", err)
	}
	if updatedAgent.HookBead != "" {
		t.Errorf("Expected HookBead to be empty, got %q", updatedAgent.HookBead)
	}

	t.Logf("Successfully cleared slot on agent %s via routing", result.Issue.ID)
}

// TestSlotFailsWithoutRouting verifies that slot commands fail without routing
// for cross-database agents (baseline behavior before fix).
func TestSlotFailsWithoutRouting(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()

	// Create only a town database (no routing)
	townBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("Failed to create town beads dir: %v", err)
	}

	townDBPath := filepath.Join(townBeadsDir, "beads.db")
	townStore := newTestStoreWithPrefix(t, townDBPath, "hq")

	// Set up global state (no routes.jsonl)
	oldDbPath := dbPath
	dbPath = townDBPath
	t.Cleanup(func() { dbPath = oldDbPath })

	// Try to resolve a gt-* agent that doesn't exist locally
	// This should fail because there's no routing configured
	_, err := resolveAgentWithRouting(ctx, townStore, "gt-nonexistent-agent")
	if err == nil {
		t.Error("Expected error when resolving non-existent agent without routing")
	}

	t.Logf("Correctly failed to resolve agent without routing: %v", err)
}

// resolveAgentWithRouting resolves an agent ID using routing if needed.
// This is the function that slot commands should use (with the fix).
func resolveAgentWithRouting(ctx context.Context, localStore storage.Storage, agentArg string) (*RoutedResult, error) {
	if needsRouting(agentArg) {
		return resolveAndGetIssueWithRouting(ctx, localStore, agentArg)
	}

	// Fallback to local store resolution
	return resolveAndGetFromStore(ctx, localStore, agentArg, false)
}
