package proxy

import (
	"bytes"
	"net"
	"testing"
)

// Hand-computed v4 reference: src 192.168.1.100:54321, dst 10.0.0.1:2108
// Signature (12) + VerCmd (0x21) + Family (0x11) + Len (0x000C) +
//   srcIP (C0 A8 01 64) + dstIP (0A 00 00 01) + srcPort (D431) + dstPort (083C)
func TestBuildV2Header_IPv4(t *testing.T) {
	src := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 54321}
	dst := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 2108}

	got, err := BuildV2Header(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte{
		// Signature (12 bytes)
		0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D,
		0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
		// VerCmd: v2 + PROXY
		0x21,
		// Family: AF_INET + TCP
		0x11,
		// Addr length: 12 (big-endian)
		0x00, 0x0C,
		// src IP: 192.168.1.100
		0xC0, 0xA8, 0x01, 0x64,
		// dst IP: 10.0.0.1
		0x0A, 0x00, 0x00, 0x01,
		// src port: 54321 (0xD431)
		0xD4, 0x31,
		// dst port: 2108 (0x083C)
		0x08, 0x3C,
	}

	if !bytes.Equal(got, expected) {
		t.Errorf("IPv4 header mismatch:\ngot:  % X\nwant: % X", got, expected)
	}
	if len(got) != 28 {
		t.Errorf("expected 28 bytes, got %d", len(got))
	}
}

func TestBuildV2Header_IPv6(t *testing.T) {
	src := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1234}
	dst := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 2108}

	got, err := BuildV2Header(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 52 {
		t.Errorf("expected 52 bytes for IPv6, got %d", len(got))
	}
	// Verify signature
	if !bytes.Equal(got[0:12], proxyV2Signature) {
		t.Errorf("signature mismatch: % X", got[0:12])
	}
	// Verify VerCmd + Family + Length
	if got[12] != 0x21 {
		t.Errorf("VerCmd = 0x%02X, want 0x21", got[12])
	}
	if got[13] != 0x21 { // AF_INET6 + TCP
		t.Errorf("Family = 0x%02X, want 0x21", got[13])
	}
	if got[14] != 0x00 || got[15] != 0x24 { // 36 = 0x0024
		t.Errorf("Length = 0x%02X%02X, want 0x0024", got[14], got[15])
	}
}

func TestBuildV2Header_NilInputs(t *testing.T) {
	if _, err := BuildV2Header(nil, nil); err == nil {
		t.Error("expected error for nil inputs")
	}
	src := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1}
	if _, err := BuildV2Header(src, nil); err == nil {
		t.Error("expected error for nil dst")
	}
	if _, err := BuildV2Header(nil, src); err == nil {
		t.Error("expected error for nil src")
	}
}

func TestBuildV2Header_UnspecOnMixedFamily(t *testing.T) {
	// IPv4 src + IPv6 dst → UNSPEC fallback
	src := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	dst := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 2222}

	got, err := BuildV2Header(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Length should be 16 (header only, no address block)
	if len(got) != 16 {
		t.Errorf("expected 16 bytes for UNSPEC, got %d", len(got))
	}
	if got[13] != proxyV2AFUnspec {
		t.Errorf("expected AF_UNSPEC, got 0x%02X", got[13])
	}
}

// TestBuildV2Header_SignatureMatches guards against accidental drift in the
// magic bytes. Any byte change here means incompatibility with every PROXY v2
// parser in the world.
func TestBuildV2Header_SignatureMatches(t *testing.T) {
	expected := []byte{
		0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D,
		0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
	}
	if !bytes.Equal(proxyV2Signature, expected) {
		t.Errorf("PROXY v2 signature drift: % X", proxyV2Signature)
	}
}

func BenchmarkBuildV2Header_IPv4(b *testing.B) {
	src := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 54321}
	dst := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 2108}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildV2Header(src, dst)
	}
}
