package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func Sign(token, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token + strconv.FormatInt(timestamp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func Verify(token, secret, signature string, timestamp int64) bool {
	expected, err := hex.DecodeString(Sign(token, secret, timestamp))
	if err != nil {
		return false
	}

	received, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	return hmac.Equal(received, expected)
}
