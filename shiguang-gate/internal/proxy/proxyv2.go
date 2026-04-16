// Package proxy implements the TCP relay and PROXY Protocol v2 header encoder.
//
// PROXY Protocol v2 is the binary form of HAProxy's PROXY Protocol, used to
// transparently carry the original client IP/port through a Layer-4 proxy.
// The downstream server reads this header BEFORE sending any bytes of its own
// so that Session.realIp can be populated for logging, bans, and audit.
//
// Spec: https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt (section 2.2)
package proxy

import (
	"encoding/binary"
	"errors"
	"net"
)

// proxyV2Signature is the fixed 12-byte magic that marks a PROXY v2 header.
// Servers that don't speak PROXY v2 will see this as garbage and should close
// the connection, which is the intended behavior (gate is mandatory).
var proxyV2Signature = []byte{
	0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D,
	0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
}

// Version + command byte: high nibble = 0x2 (v2), low nibble = 0x1 (PROXY).
const proxyV2VerCmd = 0x21

// Address family bytes.
const (
	proxyV2AFUnspec   = 0x00
	proxyV2AFINETTCP  = 0x11 // AF_INET + SOCK_STREAM
	proxyV2AFINET6TCP = 0x21 // AF_INET6 + SOCK_STREAM
)

// Address block lengths (used in the big-endian uint16 length field).
const (
	proxyV2AddrLenIPv4 = 12 // 4 + 4 + 2 + 2
	proxyV2AddrLenIPv6 = 36 // 16 + 16 + 2 + 2
)

// BuildV2Header constructs a PROXY Protocol v2 binary header announcing
// (src, dst). src is the original client address as seen by gate; dst is
// gate's local address for the upstream connection (typically the listen
// address of the gate's accept socket).
//
// Both addresses MUST be of the same IP family (v4↔v4 or v6↔v6). Mixed
// families fall back to the UNSPEC family which tells the downstream
// server "no address info" — it will accept the connection but the real
// IP will be unavailable.
//
// Returns the serialized header bytes ready to be Written to the upstream
// connection as the very first payload (before any application bytes).
func BuildV2Header(src, dst *net.TCPAddr) ([]byte, error) {
	if src == nil || dst == nil {
		return nil, errors.New("proxyv2: src and dst must both be non-nil")
	}

	srcV4 := src.IP.To4()
	dstV4 := dst.IP.To4()
	srcV6 := src.IP.To16()
	dstV6 := dst.IP.To16()

	// Both v4: 28 bytes total (16 header + 12 address block).
	if srcV4 != nil && dstV4 != nil {
		buf := make([]byte, 16+proxyV2AddrLenIPv4)
		copy(buf[0:12], proxyV2Signature)
		buf[12] = proxyV2VerCmd
		buf[13] = proxyV2AFINETTCP
		binary.BigEndian.PutUint16(buf[14:16], proxyV2AddrLenIPv4)
		copy(buf[16:20], srcV4)
		copy(buf[20:24], dstV4)
		binary.BigEndian.PutUint16(buf[24:26], uint16(src.Port))
		binary.BigEndian.PutUint16(buf[26:28], uint16(dst.Port))
		return buf, nil
	}

	// Both v6 (or mixed where neither has a v4 mapping): 52 bytes.
	if srcV6 != nil && dstV6 != nil && srcV4 == nil && dstV4 == nil {
		buf := make([]byte, 16+proxyV2AddrLenIPv6)
		copy(buf[0:12], proxyV2Signature)
		buf[12] = proxyV2VerCmd
		buf[13] = proxyV2AFINET6TCP
		binary.BigEndian.PutUint16(buf[14:16], proxyV2AddrLenIPv6)
		copy(buf[16:32], srcV6)
		copy(buf[32:48], dstV6)
		binary.BigEndian.PutUint16(buf[48:50], uint16(src.Port))
		binary.BigEndian.PutUint16(buf[50:52], uint16(dst.Port))
		return buf, nil
	}

	// Mixed v4/v6 or empty IPs: emit UNSPEC header (no addresses).
	// This signals "connection is proxied but family is unknown" — the
	// server accepts the connection but realIp will be unset.
	buf := make([]byte, 16)
	copy(buf[0:12], proxyV2Signature)
	buf[12] = proxyV2VerCmd
	buf[13] = proxyV2AFUnspec
	binary.BigEndian.PutUint16(buf[14:16], 0)
	return buf, nil
}

// TCPAddrFromAddr is a tiny helper that converts net.Addr to *net.TCPAddr
// if possible. Returns nil for non-TCP addresses (Unix sockets etc.).
func TCPAddrFromAddr(addr net.Addr) *net.TCPAddr {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr
	}
	return nil
}
