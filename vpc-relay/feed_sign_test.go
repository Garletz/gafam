package main

import (
	"testing"
)

func TestEnvelopeSignVerifyRoundtrip(t *testing.T) {
	// getNodeKeypair needs the settings table — in-memory DB.
	setupMissionStoreTest(t) // opens :memory: DB (also creates gafam_settings? ensure below)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gafam_settings (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}

	sig, ts, err := signEnvelope("+33600000000", "*", "hello federation")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	env := Envelope{
		AuthorPhone:    "+33600000000",
		RecipientPhone: "*",
		Content:        "hello federation",
		Signature:      sig,
		SignedTs:       ts,
	}
	pub := getNodePubkeyHex()
	if pub == "" {
		t.Fatal("no node pubkey")
	}
	if !verifyEnvelope(pub, env) {
		t.Error("valid signature rejected")
	}
	// Tampered content must fail.
	env.Content = "forged"
	if verifyEnvelope(pub, env) {
		t.Error("tampered envelope accepted")
	}
	// Tampered timestamp must fail.
	env.Content = "hello federation"
	env.SignedTs = ts + 1
	if verifyEnvelope(pub, env) {
		t.Error("envelope with altered signed_ts accepted")
	}
	// Garbage key must fail, not panic.
	if verifyEnvelope("deadbeef", env) {
		t.Error("invalid pubkey accepted")
	}
}
