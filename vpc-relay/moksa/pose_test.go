package moksa

import "testing"

func TestPoseBoardURL(t *testing.T) {
	qs := PoseBoard("vérifie ce lien https://example.com et dis-moi si OK")
	if len(qs) < 3 {
		t.Fatalf("expected several quests, got %d", len(qs))
	}
	hasBrowser := false
	for _, q := range qs {
		if q.Tool == "browser.status" || q.Tool == "browser.screenshot" {
			hasBrowser = true
		}
	}
	if !hasBrowser {
		t.Fatal("expected browser quests for URL demand")
	}
}

func TestCreateMission(t *testing.T) {
	m := CreateMissionFromInstruction("list files in sandbox")
	if m.ID == "" || len(m.Quests) == 0 {
		t.Fatal("mission empty")
	}
	if m.WorldCard == "" {
		t.Fatal("missing world card")
	}
	got, ok := GetMission(m.ID)
	if !ok || got.ID != m.ID {
		t.Fatal("store miss")
	}
}
