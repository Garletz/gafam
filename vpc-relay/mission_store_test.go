package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Garletz/gafam/vpc-relay/moksa"
	_ "modernc.org/sqlite"
)

func setupMissionStoreTest(t *testing.T) {
	t.Helper()
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		moksa.PersistHook = nil
		moksa.DeleteHook = nil
	})
	initMissionStore()
}

func TestMissionPersistenceRoundtrip(t *testing.T) {
	setupMissionStoreTest(t)

	m := moksa.CreateEmptyMission("persist me")
	defer moksa.DeleteMission(m.ID)

	// Row must exist right after creation.
	var data string
	if err := db.QueryRow(`SELECT data FROM moksa_missions WHERE id = ?`, m.ID).Scan(&data); err != nil {
		t.Fatalf("mission not persisted on create: %v", err)
	}
	if !strings.Contains(data, "persist me") {
		t.Errorf("persisted data missing instruction")
	}

	// AddQuest writes through.
	if _, err := moksa.AddQuest(m.ID, "q one", "suparna_vpc", "sandbox.tree", nil, nil, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT data FROM moksa_missions WHERE id = ?`, m.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var decoded moksa.Mission
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Quests) != 1 || decoded.Quests[0].Title != "q one" {
		t.Fatalf("persisted quests = %+v", decoded.Quests)
	}

	// Delete removes the row.
	moksa.DeleteMission(m.ID)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM moksa_missions WHERE id = ?`, m.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("row not deleted")
	}
}

func TestMissionRecoveryOnRestart(t *testing.T) {
	setupMissionStoreTest(t)

	m := moksa.CreateEmptyMission("interrupted")
	qm, err := moksa.AddQuest(m.ID, "running quest", "suparna_vpc", "sandbox.tree", nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	qid := qm.Quests[0].ID
	if _, err := moksa.ClaimQuest(m.ID, qid, "suparna_vpc"); err != nil {
		t.Fatal(err)
	}
	// Simulate the relay dying right now: quest is "claimed", mission "planning".
	// A fresh boot runs loadMissionsFromDB again (as main() would).
	loadMissionsFromDB()

	restored, ok := moksa.GetMission(m.ID)
	if !ok {
		t.Fatalf("mission lost after reload")
	}
	// New recovery semantics: the mission is parked as "interrupted" (not
	// cancelled) and its mid-flight quest goes back to pending so Saṃyojaka
	// can resume it (auto-resume happens in resumeInterruptedMissions).
	if restored.Status != "interrupted" {
		t.Errorf("mission status = %s, want interrupted", restored.Status)
	}
	q := restored.FindQuest(qid)
	if q == nil || q.Status != "pending" {
		t.Fatalf("quest status = %+v, want pending (resumable)", q)
	}
	if !strings.Contains(restored.Summary, "Interrupted") {
		t.Errorf("summary missing interruption note")
	}

	// Cleanup.
	moksa.DeleteMission(m.ID)
}
