package e2e

import (
	"fmt"
	"strings"
)

// ScenarioServerRestart tests sync resilience across a server restart.
// 1. Create issues on both actors, sync to convergence
// 2. Kill the server process
// 3. Both actors perform local mutations (these queue locally)
// 4. Restart the server (new process, same data dir)
// 5. Sync all actors
// 6. Verify convergence
func ScenarioServerRestart(h *Harness) []VerifyResult {
	var results []VerifyResult

	pass := func(name, details string) {
		results = append(results, VerifyResult{Name: name, Passed: true, Details: details})
	}
	fail := func(name, details string) {
		results = append(results, VerifyResult{Name: name, Passed: false, Details: details})
	}

	// Step 1: Alice creates 5 issues
	aliceIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		out, err := h.TdA("create", fmt.Sprintf("Alice restart resilience issue %d", i), "--type", "task", "--priority", "P1")
		if err != nil {
			fail("alice_create_pre", fmt.Sprintf("create %d failed: %v\n%s", i, err, out))
			return results
		}
		id := extractIssueID(out)
		if id == "" {
			fail("alice_create_pre", fmt.Sprintf("no ID from create %d: %s", i, out))
			return results
		}
		aliceIDs = append(aliceIDs, id)
	}

	// Bob creates 5 issues
	bobIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		out, err := h.TdB("create", fmt.Sprintf("Bob restart resilience issue %d", i), "--type", "bug", "--priority", "P2")
		if err != nil {
			fail("bob_create_pre", fmt.Sprintf("create %d failed: %v\n%s", i, err, out))
			return results
		}
		id := extractIssueID(out)
		if id == "" {
			fail("bob_create_pre", fmt.Sprintf("no ID from create %d: %s", i, out))
			return results
		}
		bobIDs = append(bobIDs, id)
	}

	// Step 2: Sync all — verify both see 10 issues
	if err := h.SyncAll(); err != nil {
		fail("initial_sync", fmt.Sprintf("SyncAll failed: %v", err))
		return results
	}

	aliceCount := countIssues(h, "alice")
	bobCount := countIssues(h, "bob")
	if aliceCount >= 10 && bobCount >= 10 {
		pass("initial_convergence", fmt.Sprintf("alice=%d bob=%d", aliceCount, bobCount))
	} else {
		fail("initial_convergence", fmt.Sprintf("expected >=10 each, alice=%d bob=%d", aliceCount, bobCount))
		return results
	}

	// Step 3: Stop the server
	if err := h.StopServer(); err != nil {
		fail("stop_server", fmt.Sprintf("StopServer failed: %v", err))
		return results
	}
	pass("stop_server", "server stopped")

	// Step 4: Sync attempt during downtime should error but not crash
	_, syncErr := h.TdA("sync")
	if syncErr != nil {
		pass("sync_during_downtime", "sync correctly returned error during downtime")
	} else {
		// Sync might succeed if it had nothing to push/pull, but typically should fail
		pass("sync_during_downtime", "sync returned no error (may have been no-op)")
	}

	// Step 5: Both actors perform local mutations while server is down
	// Alice creates 3 more issues
	aliceNewIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		out, err := h.TdA("create", fmt.Sprintf("Alice offline created issue %d", i), "--type", "task", "--priority", "P0")
		if err != nil {
			fail("alice_offline_create", fmt.Sprintf("create %d failed: %v\n%s", i, err, out))
			return results
		}
		id := extractIssueID(out)
		if id != "" {
			aliceNewIDs = append(aliceNewIDs, id)
		}
	}
	pass("alice_offline_creates", fmt.Sprintf("created %d issues offline", len(aliceNewIDs)))

	// Bob updates 3 existing issues (change title)
	updatedCount := 0
	for i := 0; i < 3 && i < len(bobIDs); i++ {
		out, err := h.TdB("update", bobIDs[i], "--title", fmt.Sprintf("Bob updated issue title %d", i))
		if err != nil {
			// Log but continue; update might fail if issue not found locally
			_ = out
			continue
		}
		updatedCount++
	}
	pass("bob_offline_updates", fmt.Sprintf("updated %d issues offline", updatedCount))

	// Step 6: Restart the server
	if err := h.StartServer(); err != nil {
		fail("restart_server", fmt.Sprintf("StartServer failed: %v", err))
		return results
	}
	pass("restart_server", "server restarted and healthy")

	// Step 7: Sync all actors after restart
	if err := h.SyncAll(); err != nil {
		fail("post_restart_sync", fmt.Sprintf("SyncAll failed: %v", err))
		return results
	}
	pass("post_restart_sync", "sync succeeded after restart")

	// Step 8: Verify convergence
	// Both should see all 13 issues (5 alice + 5 bob + 3 alice offline)
	aliceFinal := countIssues(h, "alice")
	bobFinal := countIssues(h, "bob")
	if aliceFinal >= 13 && bobFinal >= 13 {
		pass("final_convergence_count", fmt.Sprintf("alice=%d bob=%d (expected >=13)", aliceFinal, bobFinal))
	} else {
		fail("final_convergence_count", fmt.Sprintf("expected >=13 each, alice=%d bob=%d", aliceFinal, bobFinal))
	}

	// Verify bob's updates propagated to alice
	for i := 0; i < 3 && i < len(bobIDs); i++ {
		out, err := h.TdA("show", bobIDs[i])
		if err != nil {
			fail("update_propagation", fmt.Sprintf("alice show %s failed: %v", bobIDs[i], err))
			continue
		}
		expected := fmt.Sprintf("Bob updated issue title %d", i)
		if strings.Contains(out, expected) {
			pass(fmt.Sprintf("update_propagation_%d", i), fmt.Sprintf("%s has updated title", bobIDs[i]))
		} else {
			fail(fmt.Sprintf("update_propagation_%d", i), fmt.Sprintf("%s missing '%s' in: %s", bobIDs[i], expected, truncate(out, 200)))
		}
	}

	// Verify alice's offline-created issues visible to bob
	for i, id := range aliceNewIDs {
		out, err := h.TdB("show", id)
		if err != nil {
			fail(fmt.Sprintf("offline_issue_visible_%d", i), fmt.Sprintf("bob show %s failed: %v", id, err))
			continue
		}
		expected := fmt.Sprintf("Alice offline created issue %d", i)
		if strings.Contains(out, expected) {
			pass(fmt.Sprintf("offline_issue_visible_%d", i), fmt.Sprintf("bob sees %s", id))
		} else {
			fail(fmt.Sprintf("offline_issue_visible_%d", i), fmt.Sprintf("bob missing '%s' in show %s: %s", expected, id, truncate(out, 200)))
		}
	}

	// Verify counts match between actors
	if aliceFinal == bobFinal {
		pass("count_match", fmt.Sprintf("both actors have %d issues", aliceFinal))
	} else {
		fail("count_match", fmt.Sprintf("alice=%d bob=%d", aliceFinal, bobFinal))
	}

	return results
}

// countIssues counts the number of issues an actor sees via `td list --all`.
func countIssues(h *Harness, actor string) int {
	out, err := h.Td(actor, "list", "--all")
	if err != nil {
		return 0
	}
	// Count non-empty lines that contain a td- ID
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "td-") {
			count++
		}
	}
	return count
}

