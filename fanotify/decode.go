//go:build linux

package fanotify

import (
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// decode accepts only bytes read from our own group: fd fields are owned live
// descriptors, never arbitrary user input. Keep this boundary private.
func (g *Group) decode(p []byte) ([]*Event, error) {
	var events []*Event
	var decodeErr error
	for len(p) >= unix.FAN_EVENT_METADATA_LEN {
		length := int(binary.NativeEndian.Uint32(p[:4]))
		if length < unix.FAN_EVENT_METADATA_LEN || length > len(p) {
			// A complete fixed header still identifies an acquired event file.
			event, err := g.decodeEvent(p[:unix.FAN_EVENT_METADATA_LEN])
			events = append(events, event)
			decodeErr = errors.Join(decodeErr, err, os.ErrInvalid)
			p = nil
			break
		}
		event, err := g.decodeEvent(p[:length])
		events = append(events, event)
		decodeErr = errors.Join(decodeErr, err)
		p = p[length:]
	}
	if len(p) != 0 {
		decodeErr = errors.Join(decodeErr, os.ErrInvalid)
	}
	if decodeErr != nil {
		// Unusable framing may prevent identifying an outstanding request.
		// Releasing the group guarantees that no task waits forever for it.
		g.Close()
		for _, event := range events {
			event.Close()
		}
		return nil, &os.PathError{Op: "decode", Path: "fanotify", Err: decodeErr}
	}
	return events, nil
}

// decodeEvent always returns the file owner, even on malformed metadata, so the
// batch can release it alongside files from earlier and later framed events.
func (g *Group) decodeEvent(p []byte) (*Event, error) {
	fd := int(int32(binary.NativeEndian.Uint32(p[16:20]))) //nolint:gosec // Preserve the signed Linux fd field, including FAN_NOFD.
	event := &Event{
		group: g, fd: fd,
		Mask: Mask(binary.NativeEndian.Uint64(p[8:16])),
		PID:  int(int32(binary.NativeEndian.Uint32(p[20:24]))), //nolint:gosec // Preserve the signed Linux pid field.
	}
	if fd >= 0 {
		event.file = os.NewFile(uintptr(fd), "fanotify-event")
	}

	event.permission = event.Mask&permissions != 0
	if p[4] != unix.FANOTIFY_METADATA_VERSION {
		return event, unix.EPROTO
	}

	metadata := int(binary.NativeEndian.Uint16(p[6:8]))
	invalidMetadata := metadata < unix.FAN_EVENT_METADATA_LEN || metadata > len(p)
	missingDescriptor := fd < 0
	validOverflow := fd == unix.FAN_NOFD && event.Mask&QueueOverflow != 0 && !event.permission
	if invalidMetadata || (missingDescriptor && !validOverflow) {
		return event, os.ErrInvalid
	}

	info, err := decodeInfo(p[metadata:])
	event.Info = info
	return event, err
}

func decodeInfo(p []byte) ([]Info, error) {
	var info []Info
	for len(p) != 0 {
		if len(p) < 4 {
			return nil, os.ErrInvalid
		}
		length := int(binary.NativeEndian.Uint16(p[2:4]))
		if length < 4 || length > len(p) {
			return nil, os.ErrInvalid
		}
		info = append(info, Info{Type: p[0], Data: append([]byte(nil), p[4:length]...)})
		p = p[length:]
	}
	return info, nil
}
