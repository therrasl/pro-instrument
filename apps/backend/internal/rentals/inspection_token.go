package rentals

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateInspectionToken creates an HMAC-SHA256 signature for accessing the tool inspection view.
func GenerateInspectionToken(secret, rentalID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rentalID + ":inspection"))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateInspectionToken checks if the provided token matches the expected HMAC-SHA256 signature.
func ValidateInspectionToken(secret, rentalID, token string) bool {
	if secret == "" || rentalID == "" || token == "" {
		return false
	}
	expected := GenerateInspectionToken(secret, rentalID)
	return hmac.Equal([]byte(token), []byte(expected))
}
