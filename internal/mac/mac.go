package mac

import (
	"crypto/sha256"
	"fmt"
)



func GenerateNICMAC(vmID, nicID string) string {
	h := sha256.Sum256([]byte(vmID + "|" + nicID))
	return fmt.Sprintf("52:54:%02x:%02x:%02x:%02x", h[0], h[1], h[2], h[3])
}
