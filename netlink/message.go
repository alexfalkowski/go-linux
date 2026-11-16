//go:build linux

package netlink

import (
	"encoding/binary"
	"errors"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// Flags is a Linux nlmsg_flags mask. Meaning depends on operation and direction.
type Flags uint16

// Request identifies a request (NLM_F_REQUEST).
const Request Flags = unix.NLM_F_REQUEST

// Multi identifies a multipart reply terminated by Done (NLM_F_MULTI).
const Multi Flags = unix.NLM_F_MULTI

// Acknowledge requests a success acknowledgement (NLM_F_ACK).
const Acknowledge Flags = unix.NLM_F_ACK

// DumpInterrupted reports an inconsistent dump (NLM_F_DUMP_INTR).
const DumpInterrupted Flags = unix.NLM_F_DUMP_INTR

// DumpFiltered reports kernel-side filtering (NLM_F_DUMP_FILTERED).
const DumpFiltered Flags = unix.NLM_F_DUMP_FILTERED

// DumpFlags selects all matching objects for a GET request (NLM_F_DUMP).
const DumpFlags Flags = unix.NLM_F_DUMP

// Replace replaces an existing object for a NEW request (NLM_F_REPLACE).
const Replace Flags = unix.NLM_F_REPLACE

// Exclusive rejects an existing object for a NEW request (NLM_F_EXCL).
const Exclusive Flags = unix.NLM_F_EXCL

// Create creates an absent object for a NEW request (NLM_F_CREATE).
const Create Flags = unix.NLM_F_CREATE

// Append appends an object for a NEW request (NLM_F_APPEND).
const Append Flags = unix.NLM_F_APPEND

// Capped omits the original request payload in an ACK (NLM_F_CAPPED).
const Capped Flags = unix.NLM_F_CAPPED

// AckTLVs indicates extended acknowledgement attributes (NLM_F_ACK_TLVS).
const AckTLVs Flags = unix.NLM_F_ACK_TLVS

// Noop is the NLMSG_NOOP control message type.
const Noop uint16 = unix.NLMSG_NOOP

// ErrorMessage is the NLMSG_ERROR acknowledgement message type.
const ErrorMessage uint16 = unix.NLMSG_ERROR

// Done terminates a multipart dump (NLMSG_DONE).
const Done uint16 = unix.NLMSG_DONE

// Overrun reports lost messages (NLMSG_OVERRUN).
const Overrun uint16 = unix.NLMSG_OVERRUN

// ErrMalformed identifies invalid message, attribute, or control framing.
var ErrMalformed = errors.New("netlink: malformed message")

// ErrDumpInterrupted means a dump may omit objects or be inconsistent.
var ErrDumpInterrupted = errors.New("netlink: dump interrupted")

// ErrTruncated means a consumed datagram exceeded the configured receive size.
var ErrTruncated = errors.New("netlink: truncated datagram")

// ErrLimit means a response exceeded its configured accumulation bound.
var ErrLimit = errors.New("netlink: response limit exceeded")

// ErrUnexpectedReply identifies a sender, sequence, port, or control mismatch.
var ErrUnexpectedReply = errors.New("netlink: unexpected reply")

// MarshalMessages encodes complete messages with native-endian headers and
// four-byte alignment. Lengths are derived from Data; padding is zeroed. Values
// too large for the wire length or a Go slice match os.ErrInvalid.
func MarshalMessages(messages []Message) ([]byte, error) {
	var size int
	for _, message := range messages {
		if uint64(len(message.Data)) > math.MaxUint32-unix.NLMSG_HDRLEN || len(message.Data) > math.MaxInt-19 {
			return nil, os.ErrInvalid
		}
		n := align(unix.NLMSG_HDRLEN + len(message.Data))
		if n > math.MaxInt-size {
			return nil, os.ErrInvalid
		}
		size += n
	}

	data := make([]byte, size)
	var offset int
	for _, message := range messages {
		n := unix.NLMSG_HDRLEN + len(message.Data)
		binary.NativeEndian.PutUint32(data[offset:], uint32(n)) //nolint:gosec // Length checked above.
		binary.NativeEndian.PutUint16(data[offset+4:], message.Header.Type)
		binary.NativeEndian.PutUint16(data[offset+6:], uint16(message.Header.Flags))
		binary.NativeEndian.PutUint32(data[offset+8:], message.Header.Sequence)
		binary.NativeEndian.PutUint32(data[offset+12:], message.Header.PortID)
		copy(data[offset+unix.NLMSG_HDRLEN:], message.Data)
		offset += align(n)
	}
	return data, nil
}

// UnmarshalMessages decodes one datagram. Data slices borrow input; no partial batch
// is returned on ErrMalformed. Final alignment padding may be wholly absent;
// partial padding or trailing fragments are rejected. Padding bytes are ignored.
func UnmarshalMessages(data []byte) ([]Message, error) {
	var messages []Message
	for len(data) > 0 {
		if len(data) < unix.NLMSG_HDRLEN {
			return nil, ErrMalformed
		}

		length := uint64(binary.NativeEndian.Uint32(data))
		if length < unix.NLMSG_HDRLEN || length > uint64(len(data)) {
			return nil, ErrMalformed
		}

		n := int(length) //nolint:gosec // Bounded by the available slice above.
		messages = append(messages, Message{Header: parseHeader(data), Data: data[unix.NLMSG_HDRLEN:n:n]})
		if n == len(data) {
			break
		}
		if align(n) > len(data) {
			return nil, ErrMalformed
		}
		data = data[align(n):]
	}
	return messages, nil
}

// Header preserves nlmsghdr fields other than the derived wire length.
type Header struct {
	// Sequence matches a request; multicast notifications normally use zero.
	Sequence uint32
	// PortID is nlmsg_pid, not authenticated sender identity or necessarily a PID.
	PortID uint32
	// Type is a control type below 16 or a protocol-specific message/family ID.
	Type uint16
	// Flags preserves all received bits, including unknown flags.
	Flags Flags
}

// Message holds one nlmsghdr and its protocol-specific payload.
type Message struct {
	// Data excludes the Netlink header and alignment padding. Parsed data borrows
	// the input buffer; received data remains valid after the next operation.
	Data []byte
	// Header preserves Linux type, flags, sequence, and port ID.
	Header Header
}

func parseHeader(data []byte) Header {
	return Header{
		Type: binary.NativeEndian.Uint16(data[4:]), Flags: Flags(binary.NativeEndian.Uint16(data[6:])),
		Sequence: binary.NativeEndian.Uint32(data[8:]), PortID: binary.NativeEndian.Uint32(data[12:]),
	}
}

func align(n int) int {
	return (n + 3) &^ 3
}
