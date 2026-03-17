package crypto

import "testing"

func TestSignAndVerify(t *testing.T) {
	const (
		token     = "agent-token"
		secret    = "agent-secret"
		timestamp = int64(1710000000)
	)

	signature := Sign(token, secret, timestamp)
	if signature == "" {
		t.Fatal("signature should not be empty")
	}

	if !Verify(token, secret, signature, timestamp) {
		t.Fatal("expected signature to verify")
	}

	if Verify(token, secret, "deadbeef", timestamp) {
		t.Fatal("expected invalid signature to fail verification")
	}
}
