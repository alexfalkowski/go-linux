//go:build linux

package netlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// AckTextAttribute carries a NUL-terminated kernel diagnostic (NLMSGERR_ATTR_MSG).
const AckTextAttribute AttributeType = unix.NLMSGERR_ATTR_MSG

// AckOffsetAttribute carries the rejected request offset as a native uint32 (NLMSGERR_ATTR_OFFS).
const AckOffsetAttribute AttributeType = unix.NLMSGERR_ATTR_OFFS

// AckCookieAttribute carries a subsystem-specific binary cookie (NLMSGERR_ATTR_COOKIE).
const AckCookieAttribute AttributeType = unix.NLMSGERR_ATTR_COOKIE

// ParseAcknowledgement decodes NLMSG_ERROR or dump NLMSG_DONE, including capped
// and uncapped requests and extended TLVs. Data borrows message.Data. Negative
// kernel status returns *Error, preserving errno. DumpInterrupted additionally
// matches ErrDumpInterrupted. Framing/policy errors match ErrMalformed; unknown
// attributes remain available. Success warnings are retained even with nil error.
//
// See https://docs.kernel.org/userspace-api/netlink/intro.html#extended-ack.
func ParseAcknowledgement(message Message) (Acknowledgement, error) {
	ack, err := parseAcknowledgement(message)
	if err != nil {
		return Acknowledgement{}, err
	}
	if ack.Code != 0 {
		err = &Error{Acknowledgement: ack}
	}
	if message.Header.Flags&DumpInterrupted != 0 {
		err = errors.Join(err, ErrDumpInterrupted)
	}
	return ack, err
}

// Acknowledgement retains kernel status and extended error or warning details.
type Acknowledgement struct {
	// Request is the echoed request header for NLMSG_ERROR; nil for NLMSG_DONE.
	Request *Header
	// Offset is the rejected attribute's byte offset from the request header;
	// nil means the kernel did not supply NLMSGERR_ATTR_OFFS.
	Offset *uint32
	// Text is the kernel's diagnostic string; it may be empty or change by kernel.
	Text string
	// Cookie is subsystem-specific binary data, possibly present on success.
	Cookie []byte
	// Attributes retains every extended TLV, including policy and missing fields.
	Attributes []Attribute
	// Code is the positive errno, or zero for success.
	Code syscall.Errno
}

// Error is a negative kernel acknowledgement with its extended diagnostics.
// Use errors.Is for errno and errors.As for *Error. Diagnostic text is not stable.
type Error struct {
	// Acknowledgement contains the kernel status and borrowed diagnostic data.
	Acknowledgement Acknowledgement
}

// Error formats the errno and, when supplied, kernel diagnostic text.
func (e *Error) Error() string {
	if e.Acknowledgement.Text != "" {
		return fmt.Sprintf("netlink: %s: %s", e.Acknowledgement.Code, e.Acknowledgement.Text)
	}
	return "netlink: " + e.Acknowledgement.Code.Error()
}

// Unwrap preserves the kernel errno for errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.Acknowledgement.Code
}

func parseAcknowledgement(message Message) (Acknowledgement, error) {
	data := message.Data
	wrongType := message.Header.Type != ErrorMessage && message.Header.Type != Done
	if len(data) < 4 || wrongType {
		return Acknowledgement{}, ErrMalformed
	}

	status := int32(binary.NativeEndian.Uint32(data)) //nolint:gosec // Linux status is a signed 32-bit integer.
	if status > 0 || status < -4095 {
		return Acknowledgement{}, ErrMalformed
	}

	ack := Acknowledgement{Code: syscall.Errno(-status)}
	offset, request, err := acknowledgementOffset(message, status)
	if err != nil {
		return Acknowledgement{}, err
	}
	ack.Request = request

	if message.Header.Flags&AckTLVs != 0 {
		attributes, err := UnmarshalAttributes(data[offset:])
		if err != nil {
			return Acknowledgement{}, err
		}
		ack.Attributes = attributes
		if err := ack.decodeAttributes(); err != nil {
			return Acknowledgement{}, err
		}
	}
	return ack, nil
}

func (a *Acknowledgement) decodeAttributes() error {
	for _, attribute := range a.Attributes {
		switch attribute.ID() {
		case unix.NLMSGERR_ATTR_MSG:
			text, err := attribute.String()
			if err != nil {
				return err
			}
			a.Text = text
		case unix.NLMSGERR_ATTR_OFFS:
			offset, err := attribute.Uint32()
			if err != nil {
				return err
			}
			a.Offset = &offset
		case unix.NLMSGERR_ATTR_COOKIE:
			a.Cookie = attribute.Data
		}
	}
	return nil
}

func acknowledgementOffset(message Message, status int32) (int, *Header, error) {
	if message.Header.Type == Done {
		return 4, nil, nil
	}
	data := message.Data
	if len(data) < 4+unix.NLMSG_HDRLEN {
		return 0, nil, ErrMalformed
	}
	length := uint64(binary.NativeEndian.Uint32(data[4:]))
	if length < unix.NLMSG_HDRLEN {
		return 0, nil, ErrMalformed
	}
	header := parseHeader(data[4:])
	if status == 0 || message.Header.Flags&Capped != 0 {
		return 4 + unix.NLMSG_HDRLEN, &header, nil
	}
	available := uint64(len(data)) - 4
	if length > available {
		return 0, nil, ErrMalformed
	}
	offset := 4 + align(int(length)) //nolint:gosec // Length is bounded by the available Go slice.
	if offset > len(data) {
		return 0, nil, ErrMalformed
	}
	return offset, &header, nil
}
