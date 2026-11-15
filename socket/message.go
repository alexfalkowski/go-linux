//go:build linux

package socket

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// MessageFlags preserves recvmsg(2) output flags, including unknown bits.
type MessageFlags int

// Truncated means the payload exceeded the supplied buffer (MSG_TRUNC).
// The unread tail was discarded; N still counts only copied bytes.
const Truncated MessageFlags = unix.MSG_TRUNC

// ControlTruncated means ancillary data was discarded by Linux (MSG_CTRUNC).
const ControlTruncated MessageFlags = unix.MSG_CTRUNC

// Message describes one consumed message. Payload bytes are in Receive's buffer.
// On any error Files is nil: the receiver never owns error-path cleanup.
type Message struct {
	// From is the sender, or nil for an unnamed connected peer.
	From Address
	// Credentials contains kernel-validated SCM_CREDENTIALS, or nil when absent.
	Credentials *Credentials
	// Files are new caller-owned, close-on-exec references from SCM_RIGHTS.
	// They share offsets/status flags with the sender and must each be closed.
	Files []*os.File
	// N is the number of bytes copied into Receive's payload buffer.
	N int
	// Flags contains Linux output flags; inspect Truncated even on success.
	Flags MessageFlags
}

// Credentials contains Linux process credentials. IDs are interpreted in the
// receiving process's namespaces; they are observations, not authorization.
type Credentials struct {
	// PID is the kernel-validated message PID (sender PID by default),
	// or the connection-time peer PID.
	PID int32
	// UID is the kernel-validated message UID (real UID by default),
	// or the connection-time peer effective UID.
	UID uint32
	// GID is the kernel-validated message GID (real GID by default),
	// or the connection-time peer effective GID.
	GID uint32
}

// Send sends exactly one message using [sendmsg(2)]. To is nil for a Unix pair;
// bound Unix datagrams require a UnixAddress, and Netlink requires a
// NetlinkAddress (zero selects the kernel). Wrong families and address pointers
// match os.ErrInvalid. Unix sockets support empty messages; Netlink rejects
// empty payloads with ENODATA.
//
// Files borrows up to 253 files for SCM_RIGHTS on Unix sockets. With files,
// at least one payload byte is required. Nil files match os.ErrInvalid. Each
// input is duplicated under lifetime protection before waiting; all temporary
// references close on success or failure. Inputs remain caller-owned. Received
// references share open-file-description state. A send failure never requires
// caller error-path cleanup. Unix pairs wait for capacity, a deadline, or Close.
// Sends with an explicit Unix or Netlink destination instead return EAGAIN when
// capacity is unavailable: sender writability cannot track that destination's
// queue. Callers choose whether and when to retry. MSG_NOSIGNAL prevents SIGPIPE,
// and errno identity is preserved.
//
// [sendmsg(2)]: https://man7.org/linux/man-pages/man2/sendmsg.2.html
func (c *Conn) Send(p []byte, to Address, files ...*os.File) (int, error) {
	if c.file == nil {
		return 0, os.ErrInvalid
	}

	address, err := c.sendAddress(to)
	if err != nil {
		return 0, socketError("sendmsg", err)
	}

	if len(files) > 0 && (c.family != unix.AF_UNIX || len(p) == 0) {
		return 0, socketError("sendmsg", os.ErrInvalid)
	}

	fds, err := duplicateFiles(files)
	if err != nil {
		return 0, socketError("sendmsg", err)
	}
	defer closeDescriptors(fds)

	var control []byte
	if len(fds) > 0 {
		control = unix.UnixRights(fds...)
	}
	return c.writeMessage(p, control, address)
}

// Receive consumes one message using [recvmsg(2)], waiting for a message, a read
// deadline, or Close. MaxFiles (0..253) bounds returned SCM_RIGHTS references;
// invalid limits match os.ErrInvalid without consuming a message. Nil/empty p
// still consumes a message and reports Truncated when bytes were discarded.
// Payload truncation returns copied bytes and Truncated without an error.
//
// Credentials are decoded when present (see SetPassCredentials). Ancillary
// truncation or more than maxFiles rights returns EMSGSIZE and no files; all
// received references close, including when another decoding error occurs.
// Linux closes rights that do not fit the ancillary buffer. MSG_CMSG_CLOEXEC
// atomically protects received rights across exec. Unknown controls are ignored.
//
// Zero-length records return N=0, nil error. SeqPacket peer shutdown also
// returns N=0, nil error, as recvmsg cannot distinguish it from an empty record;
// applications needing EOF must exclude empty records in their protocol.
//
// [recvmsg(2)]: https://man7.org/linux/man-pages/man2/recvmsg.2.html
func (c *Conn) Receive(p []byte, maxFiles int) (Message, error) {
	if c.file == nil {
		return Message{}, os.ErrInvalid
	}
	if maxFiles < 0 || maxFiles > 253 {
		return Message{}, socketError("recvmsg", os.ErrInvalid)
	}

	size := unix.CmsgSpace(unix.SizeofUcred)
	if maxFiles > 0 {
		size += unix.CmsgSpace(4 * maxFiles)
	}

	control := make([]byte, size)
	var message Message
	var from Address
	var controlN, flags int
	var callErr error
	err := c.raw.Read(func(fd uintptr) bool {
		message.N, controlN, flags, from, callErr = receiveMessage(int(fd), p, control)
		return !errors.Is(callErr, unix.EAGAIN)
	})
	if err != nil {
		return Message{}, c.pollError("recvmsg", err)
	}
	message.Flags, message.From = MessageFlags(flags), from
	if err := message.decodeControl(control[:controlN]); err != nil {
		return message.withoutFiles(), socketError("recvmsg", err)
	}
	if callErr != nil {
		return message.withoutFiles(), socketError("recvmsg", callErr)
	}
	if message.Flags&ControlTruncated != 0 || len(message.Files) > maxFiles {
		return message.withoutFiles(), socketError("recvmsg", unix.EMSGSIZE)
	}
	return message, nil
}

func (c *Conn) writeMessage(p, control []byte, address Address) (int, error) {
	var n int
	var callErr error
	addressed := address != nil
	err := c.raw.Write(func(fd uintptr) bool {
		n, callErr = sendMessage(int(fd), p, control, address)
		return addressed || !errors.Is(callErr, unix.EAGAIN)
	})
	if err != nil {
		return 0, c.pollError("sendmsg", err)
	}
	if callErr != nil {
		return 0, socketError("sendmsg", callErr)
	}
	return n, nil
}

func (c *Conn) sendAddress(to Address) (Address, error) {
	switch a := to.(type) {
	case nil:
		if c.family == unix.AF_UNIX {
			return nil, nil
		}
	case UnixAddress:
		if c.family == unix.AF_UNIX && validUnixAddress(a) {
			return a, nil
		}
	case NetlinkAddress:
		if c.family == unix.AF_NETLINK {
			return a, nil
		}
	}
	return nil, os.ErrInvalid
}

func (m *Message) decodeControl(control []byte) error {
	for len(control) >= unix.CmsgLen(0) {
		header, data, rest, err := unix.ParseOneSocketControlMessage(control)
		if err != nil {
			return err
		}

		control = rest
		if header.Level != unix.SOL_SOCKET {
			continue
		}

		message := unix.SocketControlMessage{Header: header, Data: data}
		switch header.Type {
		case unix.SCM_RIGHTS:
			fds, err := unix.ParseUnixRights(&message)
			if err != nil {
				return err
			}
			for _, fd := range fds {
				m.Files = append(m.Files, os.NewFile(uintptr(fd), "socket-right"))
			}
		case unix.SCM_CREDENTIALS:
			credentials, err := unix.ParseUnixCredentials(&message)
			if err != nil {
				return err
			}
			m.Credentials = &Credentials{PID: credentials.Pid, UID: credentials.Uid, GID: credentials.Gid}
		}
	}
	return nil
}

func (m Message) withoutFiles() Message {
	for _, file := range m.Files {
		file.Close()
	}
	m.Files = nil
	return m
}

func duplicateFiles(files []*os.File) ([]int, error) {
	if len(files) > 253 {
		return nil, os.ErrInvalid
	}

	fds := make([]int, 0, len(files))
	for _, file := range files {
		if file == nil {
			closeDescriptors(fds)
			return nil, os.ErrInvalid
		}

		raw, err := file.SyscallConn()
		if err != nil {
			closeDescriptors(fds)
			return nil, err
		}

		var fd int
		var callErr error
		err = raw.Control(func(original uintptr) {
			fd, callErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0)
		})
		if err != nil || callErr != nil {
			closeDescriptors(fds)
			if err != nil {
				return nil, os.ErrClosed
			}
			return nil, callErr
		}
		fds = append(fds, fd)
	}
	return fds, nil
}

func closeDescriptors(fds []int) {
	for _, fd := range fds {
		unix.Close(fd)
	}
}

func sendMessage(fd int, p, control []byte, to Address) (int, error) {
	for {
		var n int
		var err error
		if address, ok := to.(UnixAddress); ok && address.Pathname {
			n, err = sendPathnameMessage(fd, p, control, address.Name)
		} else {
			var address unix.Sockaddr
			if to != nil {
				address = to.socketAddress()
			}
			n, err = unix.SendmsgN(fd, p, control, address, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		}
		if !errors.Is(err, unix.EINTR) {
			return n, err
		}
	}
}
