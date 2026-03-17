package crypto

import (
	"testing"
)

func TestDecryptKnownVector(t *testing.T) {
	const password = "correct horse battery staple"
	const encrypted = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaG1XanZcH2nPZ9pomUlYGII28/Tn6vDzhU+kPAqx0MyU="
	const expected = "binance-api-key"

	got, err := Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if got != expected {
		t.Fatalf("unexpected plaintext: got %q want %q", got, expected)
	}
}

func TestDecryptRejectsBadCiphertext(t *testing.T) {
	if _, err := Decrypt("bad-data", "secret"); err == nil {
		t.Fatal("expected invalid ciphertext to fail")
	}
}
