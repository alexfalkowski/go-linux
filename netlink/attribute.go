//go:build linux

package netlink

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// AttributeType includes the 14-bit attribute ID and Linux NLA_F_* flags.
type AttributeType uint16

// Nested marks an attribute containing further attributes (NLA_F_NESTED).
const Nested AttributeType = unix.NLA_F_NESTED

// NetworkByteOrder marks a big-endian scalar (NLA_F_NET_BYTEORDER).
const NetworkByteOrder AttributeType = unix.NLA_F_NET_BYTEORDER

// MarshalAttributes encodes native-endian nlattr headers with zeroed alignment
// padding. Attribute data is copied verbatim. An attribute payload above 65531
// bytes, or a total length overflowing a Go slice, matches os.ErrInvalid.
func MarshalAttributes(attributes []Attribute) ([]byte, error) {
	var size int
	for _, attribute := range attributes {
		if len(attribute.Data) > math.MaxUint16-4 {
			return nil, os.ErrInvalid
		}
		n := align(4 + len(attribute.Data))
		if n > math.MaxInt-size {
			return nil, os.ErrInvalid
		}
		size += n
	}

	data := make([]byte, size)
	var offset int
	for _, attribute := range attributes {
		n := 4 + len(attribute.Data)
		binary.NativeEndian.PutUint16(data[offset:], uint16(n)) //nolint:gosec // Attribute length checked above.
		binary.NativeEndian.PutUint16(data[offset+2:], uint16(attribute.Type))
		copy(data[offset+4:], attribute.Data)
		offset += align(n)
	}
	return data, nil
}

// UnmarshalAttributes decodes a single attribute level, preserving order, duplicate
// IDs, unknown IDs, and type flags. Data borrows input. Framing and final padding
// follow UnmarshalMessages; malformed input returns nil, ErrMalformed. Nested data
// is parsed only when requested, so there is no implicit recursion.
func UnmarshalAttributes(data []byte) ([]Attribute, error) {
	var attributes []Attribute
	for len(data) > 0 {
		if len(data) < 4 {
			return nil, ErrMalformed
		}

		n := int(binary.NativeEndian.Uint16(data))
		if n < 4 || n > len(data) {
			return nil, ErrMalformed
		}

		attributes = append(attributes, Attribute{Type: AttributeType(binary.NativeEndian.Uint16(data[2:])), Data: data[4:n:n]})

		if n == len(data) {
			break
		}
		if align(n) > len(data) {
			return nil, ErrMalformed
		}
		data = data[align(n):]
	}
	return attributes, nil
}

// Attribute holds one nlattr. Family-specific policy decides which IDs, flags,
// repetitions, and payloads are valid; framing alone does not enforce policy.
type Attribute struct {
	// Data excludes the attribute header and alignment padding.
	Data []byte
	// Type retains ID and NLA_F_* flags, even when the ID is unknown.
	Type AttributeType
}

// ID returns Type with the nesting and network-byte-order flags removed.
func (a Attribute) ID() uint16 {
	return uint16(a.Type &^ (Nested | NetworkByteOrder))
}

// Uint16 decodes an exact-width scalar, using big endian when NetworkByteOrder
// is present and native endian otherwise. Wrong widths match ErrMalformed.
func (a Attribute) Uint16() (uint16, error) {
	if len(a.Data) != 2 {
		return 0, ErrMalformed
	}
	return a.byteOrder().Uint16(a.Data), nil
}

// Uint32 follows Uint16's byte-order and exact-width rules for a 32-bit value.
func (a Attribute) Uint32() (uint32, error) {
	if len(a.Data) != 4 {
		return 0, ErrMalformed
	}
	return a.byteOrder().Uint32(a.Data), nil
}

// Uint64 follows Uint16's byte-order and exact-width rules for a 64-bit value.
func (a Attribute) Uint64() (uint64, error) {
	if len(a.Data) != 8 {
		return 0, ErrMalformed
	}
	return a.byteOrder().Uint64(a.Data), nil
}

// String decodes a NUL-terminated string, rejecting absent terminators and
// interior NULs with ErrMalformed. Binary attributes should use Data directly.
func (a Attribute) String() (string, error) {
	if len(a.Data) == 0 {
		return "", ErrMalformed
	}

	end := len(a.Data) - 1
	terminated := a.Data[end] == 0
	containsNUL := bytes.IndexByte(a.Data[:end], 0) >= 0
	if !terminated || containsNUL {
		return "", ErrMalformed
	}
	return string(a.Data[:end]), nil
}

func (a Attribute) byteOrder() binary.ByteOrder {
	if a.Type&NetworkByteOrder != 0 {
		return binary.BigEndian
	}
	return binary.NativeEndian
}
