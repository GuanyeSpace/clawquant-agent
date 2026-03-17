package crypto

import (
	"testing"
)

func TestDecryptKnownVector(t *testing.T) {
	const password = "mypassword"
	const encrypted = "ctTrtaWwLBT5oRfZl3ulRDH9ODeCByeLCEfk7fi/GtXb4GSdcHmTUPe7XcmbRCHkur3U6airqhE="
	const expected = "test-api-key"

	got, err := Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if got != expected {
		t.Fatalf("unexpected plaintext: got %q want %q", got, expected)
	}
}

func TestDecryptRejectsWrongPassword(t *testing.T) {
	const encrypted = "ctTrtaWwLBT5oRfZl3ulRDH9ODeCByeLCEfk7fi/GtXb4GSdcHmTUPe7XcmbRCHkur3U6airqhE="

	if _, err := Decrypt(encrypted, "wrong-password"); err == nil {
		t.Fatal("expected wrong password to fail")
	}
}

func TestDecryptRejectsBadCiphertext(t *testing.T) {
	if _, err := Decrypt("bad-data", "secret"); err == nil {
		t.Fatal("expected invalid ciphertext to fail")
	}
}

func TestDecryptRejectsCorruptedCiphertext(t *testing.T) {
	const encrypted = "ctTrtaWwLBT5oRfZl3ulRDH9ODeCByeLCEfk7fi/GtXb4GSdcHmTUPe7XcmbRCHkur3U6airqhE="

	if _, err := Decrypt(encrypted[:len(encrypted)-4]+"AAAA", "mypassword"); err == nil {
		t.Fatal("expected corrupted ciphertext to fail")
	}
}
