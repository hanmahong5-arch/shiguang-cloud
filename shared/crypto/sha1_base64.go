package crypto

import (
	"crypto/sha1"
	"encoding/base64"
)

// SHA1Base64 computes SHA-1 of the UTF-8 bytes of input and returns standard
// Base64 encoding. This matches the Beyond 4.8 / AL-Aion password storage
// format used in the `account_data` table of `al_server_ls`.
//
// Reference: AionNetGate/AionNetGate/Services/AccountService.cs EncodeBySHA1.
func SHA1Base64(password string) string {
	sum := sha1.Sum([]byte(password))
	return base64.StdEncoding.EncodeToString(sum[:])
}
