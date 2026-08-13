package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/Garletz/gafam/vpc-relay/moksa"
)

type questTiming struct {
	start, end time.Time
}

var (
	timingMu sync.Mutex
	timings  = map[string]*questTiming{}
)

func setupLevelTest(t *testing.T) {
	t.Helper()
	// No DB writes during these tests.
	oldP, oldD := moksa.PersistHook, moksa.DeleteHook
	moksa.PersistHook, moksa.DeleteHook = nil, nil
	t.Cleanup(func() { moksa.PersistHook, moksa.DeleteHook = oldP, oldD })

	timingMu.Lock()
	timings = map[string]*questTiming{}
	timingMu.Unlock()

	karaka.RegisterTool(karaka.Tool{
		ID:       "test.sleep",
		Category: "test",
		Params:   map[string]karaka.ParamSpec{},
		Returns:  "{}",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			marker, _ := params["marker"].(string)
			timingMu.Lock()
			timings[marker] = &questTiming{start: time.Now()}
			timingMu.Unlock()
			time.Sleep(150 * time.Millisecond)
			timingMu.Lock()
			timings[marker].end = time.Now()
			timingMu.Unlock()
			return map[string]interface{}{"marker": marker}, nil
		},
	})
	karaka.RegisterTool(karaka.Tool{
		ID:       "test.fail",
		Category: "test",
		Params:   map[string]karaka.ParamSpec{},
		Returns:  "{}",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			return nil, fmt.Errorf("boom")
		},
	})
	karaka.RegisterKaraka(karaka.Karaka{
		ID:     "test_karaka",
		Name:   "Test",
		Tier:   "L1",
		Status: "idle",
		Tools:  map[string]string{"test.sleep": "allow", "test.fail": "allow"},
	})
}

func addLevelQuest(t *testing.T, missionID, title, tool, marker string, deps []string) string {
	t.Helper()
	m, err := moksa.AddQuest(missionID, title, "test_karaka", tool,
		map[string]interface{}{"marker": marker}, deps, 10)
	if err != nil {
		t.Fatalf("AddQuest: %v", err)
	}
	return m.Quests[len(m.Quests)-1].ID
}

func TestParallelLevels(t *testing.T) {
	setupLevelTest(t)
	m := moksa.CreateEmptyMission("parallel test")
	defer moksa.DeleteMission(m.ID)

	q1 := addLevelQuest(t, m.ID, "q1", "test.sleep", "q1", nil)
	q2 := addLevelQuest(t, m.ID, "q2", "test.sleep", "q2", nil)
	q3 := addLevelQuest(t, m.ID, "q3", "test.sleep", "q3", []string{q1, q2})
	_ = addLevelQuest(t, m.ID, "q4", "test.sleep", "q4", []string{q3})

	started := time.Now()
	failed := executeQuestLevels(context.Background(), m.ID, "test_karaka", false)
	elapsed := time.Since(started)

	if len(failed) != 0 {
		t.Fatalf("unexpected failures: %v", failed)
	}
	timingMu.Lock()
	defer timingMu.Unlock()
	for _, id := range []string{"q1", "q2", "q3", "q4"} {
		if timings[id] == nil {
			t.Fatalf("%s never ran", id)
		}
	}
	// q1 and q2 must overlap (parallel).
	if !timings["q1"].end.After(timings["q2"].start) || !timings["q2"].end.After(timings["q1"].start) {
		t.Errorf("q1/q2 did not run in parallel: %v vs %v", timings["q1"], timings["q2"])
	}
	// q3 after both, q4 after q3.
	latestQ12 := timings["q1"].end
	if timings["q2"].end.After(latestQ12) {
		latestQ12 = timings["q2"].end
	}
	if timings["q3"].start.Before(latestQ12.Add(-20 * time.Millisecond)) {
		t.Errorf("q3 started before q1+q2 finished")
	}
	if timings["q4"].start.Before(timings["q3"].end.Add(-20 * time.Millisecond)) {
		t.Errorf("q4 started before q3 finished")
	}
	// Sequential would be ~600ms; parallel ≈ 450ms. Leave slack for CI.
	if elapsed > 580*time.Millisecond {
		t.Errorf("too slow — parallelism broken? elapsed=%v", elapsed)
	}
}

func TestDepFailureCancels(t *testing.T) {
	setupLevelTest(t)
	m := moksa.CreateEmptyMission("failure test")
	defer moksa.DeleteMission(m.ID)

	q1 := addLevelQuest(t, m.ID, "q1", "test.fail", "q1", nil)
	q2 := addLevelQuest(t, m.ID, "q2", "test.sleep", "q2", []string{q1})
	q3 := addLevelQuest(t, m.ID, "q3", "test.sleep", "q3", nil)

	failed := executeQuestLevels(context.Background(), m.ID, "test_karaka", false)

	if !failed[q1] || !failed[q2] {
		t.Errorf("q1 and q2 should be failed/cancelled: %v", failed)
	}
	if failed[q3] {
		t.Errorf("q3 should have succeeded: %v", failed)
	}
	final, _ := moksa.GetMission(m.ID)
	st := map[string]string{}
	for _, q := range final.Quests {
		st[q.ID] = q.Status
	}
	if st[q2] != "cancelled" {
		t.Errorf("q2 status = %s, want cancelled", st[q2])
	}
	if st[q3] != "done" {
		t.Errorf("q3 status = %s, want done", st[q3])
	}
}

func TestApprovalModeParksAskQuests(t *testing.T) {
	setupLevelTest(t)
	// test_karaka: test.sleep becomes "ask" for this test.
	karaka.SetPermissions("test_karaka", map[string]string{"test.sleep": "ask", "test.fail": "allow"})
	t.Cleanup(func() {
		karaka.SetPermissions("test_karaka", map[string]string{"test.sleep": "allow"})
	})

	m := moksa.CreateEmptyMission("approval test")
	defer moksa.DeleteMission(m.ID)

	askQ := addLevelQuest(t, m.ID, "needs-human", "test.sleep", "ask1", nil)
	depQ := addLevelQuest(t, m.ID, "blocked-behind-approval", "test.fail", "dep1", []string{askQ})

	failed := executeQuestLevels(context.Background(), m.ID, "test_karaka", true)

	final, _ := moksa.GetMission(m.ID)
	st := map[string]string{}
	for _, q := range final.Quests {
		st[q.ID] = q.Status
	}
	if st[askQ] != "waiting_approval" {
		t.Errorf("ask quest status = %s, want waiting_approval", st[askQ])
	}
	// The dependent quest must NOT be cancelled as a "cycle" — it waits.
	if st[depQ] == "cancelled" {
		t.Errorf("dependent quest was wrongly cancelled (cycle detection behind approval)")
	}
	if failed[askQ] {
		t.Errorf("ask quest should not be marked failed: %v", failed)
	}
	// The parked quest must never have run.
	timingMu.Lock()
	_, ran := timings["ask1"]
	timingMu.Unlock()
	if ran {
		t.Errorf("ask quest executed despite approval mode")
	}
}

func TestAutoWireDependenciesFromInterpolation(t *testing.T) {
	// A planner that references {{qN...}} without depends_on must get the
	// dependency auto-wired — otherwise the quests run in the same level and
	// the reference interpolates to nothing (observed in production).
	quests := []plannedQuest{
		{Title: "fetch sms", Tool: "sms.history", Params: map[string]interface{}{}},
		{Title: "analyze", Tool: "llm.chat", Params: map[string]interface{}{
			"prompt": "analyse: {{q1.result}} et {{q3.result.stdout}}",
		}},
		{Title: "store", Tool: "vault.remember", Params: map[string]interface{}{
			"body": "{{q2.result.content}}", "title": "x",
		}, DependsOn: []int{2}},
		{Title: "self ref ignored", Tool: "llm.chat", Params: map[string]interface{}{
			"prompt": "{{q4.result}}", // self-reference must be ignored
		}},
	}
	autoWireDeps(quests)

	if len(quests[1].DependsOn) != 2 || quests[1].DependsOn[0] != 1 || quests[1].DependsOn[1] != 3 {
		t.Errorf("q2 deps = %v, want [1 3]", quests[1].DependsOn)
	}
	if len(quests[2].DependsOn) != 1 || quests[2].DependsOn[0] != 2 {
		t.Errorf("q3 deps = %v, want [2] (unchanged)", quests[2].DependsOn)
	}
	if len(quests[3].DependsOn) != 0 {
		t.Errorf("q4 deps = %v, want [] (self-ref ignored)", quests[3].DependsOn)
	}
}

func TestDepCycleCancels(t *testing.T) {
	setupLevelTest(t)
	m := moksa.CreateEmptyMission("cycle test")
	defer moksa.DeleteMission(m.ID)

	q1 := addLevelQuest(t, m.ID, "q1", "test.sleep", "q1", nil)
	q2 := addLevelQuest(t, m.ID, "q2", "test.sleep", "q2", []string{q1})
	// Create the cycle by rewiring q1 to depend on q2.
	_, _ = moksa.UpdateMission(m.ID, func(miss *moksa.Mission) error {
		if q := miss.FindQuest(q1); q != nil {
			q.DependsOn = []string{q2}
		}
		return nil
	})

	executeQuestLevels(context.Background(), m.ID, "test_karaka", false)

	final, _ := moksa.GetMission(m.ID)
	for _, q := range final.Quests {
		if q.Status != "cancelled" {
			t.Errorf("quest %s status = %s, want cancelled (cycle)", q.ID, q.Status)
		}
	}
}
