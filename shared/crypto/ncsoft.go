// Package crypto provides cryptographic primitives used by the AION server lines.
//
// NCSoftHash ports the password hash algorithm from the canonical C# reference
// (AionNetGate/AionNetGate/Services/AccountService.cs, GetAccountPasswordHash).
// The algorithm is used by AionCore (5.8) authentication to store and verify
// passwords in the `user_auth` / `account_data` tables (NCSoft original schema).
//
// Byte-level output parity with the C# reference is enforced by ncsoft_test.go
// using 15 vectors generated from the reference implementation itself.
package crypto

import (
	"encoding/hex"
	"strings"
)

// NCSoftHash computes the NCSoft password hash for the given ASCII password.
// Returns "0x" + 32 uppercase hex chars (16-byte digest).
// Input passwords longer than 16 characters are truncated to 16 (matching
// the fixed 17-byte buffer in the C# reference; byte 0 is reserved).
func NCSoftHash(password string) string {
	digest := accountPasswordHash(password)
	return "0x" + strings.ToUpper(hex.EncodeToString(digest))
}

// accountPasswordHash is the direct port of C# GetAccountPasswordHash.
// Variable names (buffer, src, num, num2..num5) intentionally mirror the
// original to make cross-verification with the C# source trivial.
func accountPasswordHash(input string) []byte {
	// 17-byte buffers; index 0 is reserved (unused) to match the 1-indexed
	// arithmetic in the C# algorithm.
	buffer := make([]byte, 17)
	src := make([]byte, 17)

	// Copy input ASCII into buffer[1..] / src[1..]. Truncate to 16 bytes.
	n := len(input)
	if n > 16 {
		n = 16
	}
	for i := 0; i < n; i++ {
		buffer[i+1] = input[i]
		src[i+1] = buffer[i+1]
	}

	// Block 0 : bytes [1..4]
	num := int64(buffer[1]) + int64(buffer[2])*0x100 +
		int64(buffer[3])*0x10000 + int64(buffer[4])*0x1000000
	num2 := (num*0x3407f + 0x269735) & 0xFFFFFFFF

	// Block 1 : bytes [5..8]
	num = int64(buffer[5]) + int64(buffer[6])*0x100 +
		int64(buffer[7])*0x10000 + int64(buffer[8])*0x1000000
	num3 := (num*0x340ff + 0x269741) & 0xFFFFFFFF

	// Block 2 : bytes [9..12]
	num = int64(buffer[9]) + int64(buffer[10])*0x100 +
		int64(buffer[11])*0x10000 + int64(buffer[12])*0x1000000
	num4 := (num*0x340d3 + 0x269935) & 0xFFFFFFFF

	// Block 3 : bytes [13..16]
	num = int64(buffer[13]) + int64(buffer[14])*0x100 +
		int64(buffer[15])*0x10000 + int64(buffer[16])*0x1000000
	num5 := (num*0x3433d + 0x269acd) & 0xFFFFFFFF

	// Write the 4 results back into buffer as little-endian uint32 blocks.
	writeLE32(buffer, 1, num2)
	writeLE32(buffer, 5, num3)
	writeLE32(buffer, 9, num4)
	writeLE32(buffer, 13, num5)

	// XOR chain: src[1] = src[1] ^ buffer[1]; then src[i] = src[i] ^ src[i-1] ^ buffer[i]
	src[1] = src[1] ^ buffer[1]
	for i := 2; i <= 16; i++ {
		src[i] = src[i] ^ src[i-1] ^ buffer[i]
	}

	// Replace any zero bytes in src[1..16] with 0x66 (algorithm artifact).
	for i := 1; i <= 16; i++ {
		if src[i] == 0 {
			src[i] = 0x66
		}
	}

	// Return src[1..16] as the 16-byte digest.
	dst := make([]byte, 16)
	copy(dst, src[1:17])
	return dst
}

// writeLE32 writes a 32-bit value as 4 little-endian bytes starting at offset.
// The C# code unpacks via explicit division by 0x1000000 / 0x10000 / 0x100
// which is equivalent to LE byte layout. Kept as a dedicated helper for clarity.
func writeLE32(buf []byte, offset int, value int64) {
	buf[offset+0] = byte(value & 0xFF)
	buf[offset+1] = byte((value >> 8) & 0xFF)
	buf[offset+2] = byte((value >> 16) & 0xFF)
	buf[offset+3] = byte((value >> 24) & 0xFF)
}
