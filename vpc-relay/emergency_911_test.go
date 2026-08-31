package main

import "testing"

func TestBuildParse911RelayRoundTrip(t *testing.T) {
	body := build911RelayBody("alert-42", 2, "+33612345678", "Alice | Mère", "Je suis bloqué, au secours")
	alertID, origPhone, origName, message, hop, ok := parse911Relay(body)
	if !ok {
		t.Fatalf("expected parse ok, got %q", body)
	}
	if alertID != "alert-42" || hop != 2 || origPhone != "+33612345678" {
		t.Fatalf("unexpected fields: id=%q hop=%d phone=%q", alertID, hop, origPhone)
	}
	// "|" inside the name must have been sanitized to "/"
	if origName != "Alice / Mère" {
		t.Fatalf("unexpected sanitized name: %q", origName)
	}
	if message != "Je suis bloqué, au secours" {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestParse911RelayRejectsGarbage(t *testing.T) {
	for _, body := range []string{"", "hello world", "GAFAM911 only2", "GAFAM911 id hop x | n"} {
		if _, _, _, _, _, ok := parse911Relay(body); ok {
			t.Fatalf("expected parse failure for %q", body)
		}
	}
	// Too few fields / bad hop
	if _, _, _, _, _, ok := parse911Relay("GAFAM911 id 0 +33 | n | m"); ok {
		t.Fatal("expected hop >= 1 requirement")
	}
	// Message may contain spaces
	alertID, _, _, msg, hop, ok := parse911Relay("GAFAM911 id 1 +33 | n | m extra words")
	if !ok || alertID != "id" || hop != 1 || msg != "m extra words" {
		t.Fatalf("expected valid parse, got id=%q hop=%d msg=%q ok=%v", alertID, hop, msg, ok)
	}
}

func TestContainsCodeWord(t *testing.T) {
	if !containsCodeWord("911 je suis en danger", "911") {
		t.Fatal("expected match")
	}
	if !containsCodeWord("URGENT 911", "911") {
		t.Fatal("expected match")
	}
	if containsCodeWord("le numero 9112 n'est pas bon", "911") {
		t.Fatal("expected no match on substring")
	}
	if containsCodeWord("", "911") {
		t.Fatal("expected no match on empty body")
	}
}

func TestMessageAfterCode(t *testing.T) {
	if got := messageAfterCode("911 bloqué à la cave", "911"); got != "bloqué à la cave" {
		t.Fatalf("unexpected: %q", got)
	}
	if got := messageAfterCode("URGENCE_GAFAM", "URGENCE_GAFAM"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
