package auth

import "testing"

func TestHashPasswordAcceptsSixCharacters(t *testing.T) {
	hash, err := HashPassword("huneng")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !VerifyPassword(hash, "huneng") {
		t.Fatal("VerifyPassword rejected the original password")
	}
}

func TestHashPasswordRejectsFiveCharacters(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("HashPassword accepted a password shorter than six characters")
	}
}
