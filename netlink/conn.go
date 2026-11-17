//go:build linux

package netlink

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/alexfalkowski/go-linux/socket"
	"golang.org/x/sys/unix"
)

// Options bounds memory use and selects kernel validation. Zero uses defaults.
type Options struct {
	// DatagramSize is the maximum consumed datagram in bytes, including headers
	// and padding. Zero means 65536; smaller than 16 or above 2^31-1 is invalid.
	DatagramSize int
	// MaxResponseBytes bounds total received wire bytes per Execute or Dump.
	// Zero means 16 MiB; values below 16 are invalid. Listeners ignore this field.
	MaxResponseBytes int
	// StrictCheck enables NETLINK_GET_STRICT_CHK (principally route GET/dumps).
	StrictCheck bool
}

func (options Options) normalized() (Options, error) {
	if options.DatagramSize == 0 {
		options.DatagramSize = 65536
	}
	if options.MaxResponseBytes == 0 {
		options.MaxResponseBytes = 16 << 20
	}
	invalidDatagramSize := options.DatagramSize < unix.NLMSG_HDRLEN || uint64(options.DatagramSize) > 1<<31-1
	invalidResponseLimit := options.MaxResponseBytes < unix.NLMSG_HDRLEN
	if invalidDatagramSize || invalidResponseLimit {
		return Options{}, os.ErrInvalid
	}

	return options, nil
}

// Open binds a request connection using socket.OpenNetlink. Protocol is normally
// socket.Generic or socket.Route. Extended and capped acknowledgements are
// required and enabled; option failures close the socket and return no owner.
// Open does not join multicast groups. See [netlink(7)].
//
// [netlink(7)]: https://man7.org/linux/man-pages/man7/netlink.7.html
func Open(protocol socket.Protocol, options Options) (*Conn, error) {
	transport, options, err := open(protocol, options)
	if err != nil {
		return nil, err
	}

	address, err := transport.LocalAddress()
	if err != nil {
		transport.Close()
		return nil, err
	}
	return &Conn{conn: transport, options: options, portID: address.(socket.NetlinkAddress).PortID, protocol: protocol}, nil
}

// Conn owns a request socket. Execute and Dump are safe concurrently, but run
// serially on each connection; use separate connections for parallel requests.
// Close and deadline setters are safe during an exchange. A deadline applies to
// I/O, not time waiting for the request lock. No hidden goroutine is started.
// The zero value returns os.ErrInvalid; valid operations after Close match
// os.ErrClosed. Do not copy after use.
type Conn struct {
	conn     *socket.Conn
	options  Options
	protocol socket.Protocol
	mu       sync.Mutex
	portID   uint32
	sequence uint32
}

// Execute sends one protocol-specific request and waits for its acknowledgement
// (and NLMSG_DONE if the kernel starts a multipart reply). Type must be >=16;
// Flags may contain Request, Acknowledge, Replace, Exclusive, Create, or Append.
// Sequence and PortID must be zero: the connection assigns increasing sequence
// numbers, skipping zero on uint32 wrap. Invalid input
// matches os.ErrInvalid. Request/Acknowledge are always added; use Dump for GET
// dumps because modifier bits overlap with mutation flags.
//
// Replies match the kernel sender, request sequence, and local header port ID
// (or zero). Response preserves received data and diagnostics, including partial
// data on error. A lost, malformed, oversized, unexpected, or timed-out exchange
// closes the connection to prevent unread replies affecting later requests.
// A completed kernel rejection or interrupted dump leaves it usable. Send
// errors retain errno (including EAGAIN) and leave the connection open. An error
// never establishes that a requested kernel mutation did not take effect.
func (c *Conn) Execute(request Message) (Response, error) {
	return c.exchange(request, false)
}

// Dump adds NLM_F_DUMP and reads through NLMSG_DONE, even if an ACK arrives first.
// Only Request, Acknowledge, and DumpFlags are accepted as input flags. The other
// input, failure, and lifetime rules are Execute's. Any NLM_F_DUMP_INTR in a
// matched message returns ErrDumpInterrupted after draining the dump. Results
// are not atomic snapshots, and interrupted dumps are never retried implicitly.
func (c *Conn) Dump(request Message) (Response, error) {
	return c.exchange(request, true)
}

// Close releases the connection and interrupts pending I/O. Repeated Close
// matches os.ErrClosed. Queued requests subsequently observe the closed socket.
func (c *Conn) Close() error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.Close()
}

// SetDeadline bounds pending/future request I/O. Zero clears both deadlines;
// expiry matches os.ErrDeadlineExceeded and an incomplete exchange closes Conn.
func (c *Conn) SetDeadline(deadline time.Time) error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.SetDeadline(deadline)
}

func (c *Conn) exchange(request Message, dump bool) (Response, error) {
	if err := validateRequest(request.Header, dump); err != nil {
		return Response{}, err
	}

	if c.conn == nil {
		return Response{}, os.ErrInvalid
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.sequence++
	if c.sequence == 0 {
		c.sequence++
	}

	request.Header.Sequence = c.sequence
	request.Header.Flags |= Request | Acknowledge
	if dump {
		request.Header.Flags |= DumpFlags
	}

	data, err := MarshalMessages([]Message{request})
	if err != nil {
		return Response{}, err
	}

	if _, err := c.conn.Send(data, socket.NetlinkAddress{}); err != nil {
		return Response{}, err
	}

	response, err := c.readResponse(request.Header, dump)
	if !response.Complete {
		c.Close()
	}
	return response, err
}

func (c *Conn) readResponse(request Header, dump bool) (Response, error) {
	state := responseState{request: request, portID: c.portID, multipart: dump}
	var received int

	for !state.response.Complete {
		messages, n, err := receive(c.conn, c.options.DatagramSize)
		if err != nil {
			return state.result(err)
		}

		// Account for all wire bytes, including controls and alignment padding.
		remaining := c.options.MaxResponseBytes - received
		if n > remaining {
			return state.result(ErrLimit)
		}
		received += n

		for _, message := range messages {
			if err := state.accept(message); err != nil {
				return state.result(err)
			}
		}
	}
	return state.result(nil)
}

// Response retains data messages and terminal controls from a single exchange.
// All data stays valid after later calls and Close; the caller owns no resources.
type Response struct {
	// Acknowledgement is the success/error ACK, or nil if none was received.
	Acknowledgement *Acknowledgement
	// Done is the multipart terminal status, or nil if none was received.
	Done *Acknowledgement
	// Messages retains protocol-specific replies, excluding control messages.
	Messages []Message
	// Complete means a terminal ACK or dump completion was received. It does not
	// imply success, consistency, or an atomic snapshot; always check the error.
	Complete bool
}

// responseState owns the protocol state that survives across datagrams. Keep
// this separate from Response, which is only the caller's collected result.
type responseState struct {
	response    Response
	request     Header
	portID      uint32
	multipart   bool
	interrupted bool
}

func (s *responseState) accept(message Message) error {
	header := message.Header
	wrongSequence := header.Sequence != s.request.Sequence
	wrongPort := header.PortID != s.portID && header.PortID != 0
	if s.response.Complete || wrongSequence || wrongPort {
		s.response.Complete = false
		return ErrUnexpectedReply
	}

	// The kernel can start a multipart reply to an ordinary request. Any
	// interruption marker must survive until terminal status, even across reads.
	s.multipart = s.multipart || header.Flags&Multi != 0
	s.interrupted = s.interrupted || header.Flags&DumpInterrupted != 0
	return s.consume(message)
}

func (s *responseState) consume(message Message) error {
	switch message.Header.Type {
	case Noop:
		return nil
	case Overrun:
		return unix.ENOBUFS
	case ErrorMessage, Done:
		return s.acknowledge(message)
	default:
		if message.Header.Type < unix.NLMSG_MIN_TYPE {
			return ErrUnexpectedReply
		}
		s.response.Messages = append(s.response.Messages, message)
		return nil
	}
}

func (s *responseState) acknowledge(message Message) error {
	if message.Header.Type == Done && !s.multipart {
		return ErrUnexpectedReply
	}

	ack, err := ParseAcknowledgement(message)
	if errors.Is(err, ErrMalformed) {
		return err
	}
	if ack.Request != nil && *ack.Request != s.request {
		return ErrUnexpectedReply
	}

	if message.Header.Type == Done {
		s.response.Done = &ack
		s.response.Complete = true
	} else {
		s.response.Acknowledgement = &ack
		// A success ACK finishes an ordinary request. A dump must still reach
		// DONE; a negative ACK is a terminal rejection in either mode.
		s.response.Complete = ack.Code != 0 || !s.multipart
	}
	if !s.response.Complete {
		return nil
	}
	return err
}

func (s *responseState) result(err error) (Response, error) {
	if s.interrupted && !errors.Is(err, ErrDumpInterrupted) {
		err = errors.Join(err, ErrDumpInterrupted)
	}
	return s.response, err
}

func open(protocol socket.Protocol, options Options) (*socket.Conn, Options, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, Options{}, err
	}
	conn, err := socket.OpenNetlink(protocol, socket.NetlinkAddress{})
	if err != nil {
		return nil, Options{}, err
	}

	for _, set := range []func(bool) error{conn.SetExtendedAck, conn.SetCapAck} {
		if err := set(true); err != nil {
			conn.Close()
			return nil, Options{}, err
		}
	}

	if options.StrictCheck {
		if err := conn.SetStrictCheck(true); err != nil {
			conn.Close()
			return nil, Options{}, err
		}
	}
	return conn, options, nil
}

func receive(conn *socket.Conn, size int) ([]Message, int, error) {
	buffer := make([]byte, size)
	message, err := conn.Receive(buffer, 0)
	if err != nil {
		return nil, 0, err
	}

	if message.Flags&socket.Truncated != 0 {
		return nil, message.N, ErrTruncated
	}
	if address, ok := message.From.(socket.NetlinkAddress); !ok || address.PortID != 0 {
		return nil, message.N, ErrUnexpectedReply
	}
	if message.N == 0 {
		return nil, 0, ErrMalformed
	}
	messages, err := UnmarshalMessages(buffer[:message.N])
	return messages, message.N, err
}

func validateRequest(header Header, dump bool) error {
	var allowed Flags
	if dump {
		allowed = Request | Acknowledge | DumpFlags
	} else {
		allowed = Request | Acknowledge | Replace | Exclusive | Create | Append
	}

	reservedType := header.Type < unix.NLMSG_MIN_TYPE
	unknownFlags := header.Flags & ^allowed != 0
	callerAssignedIdentity := header.Sequence != 0 || header.PortID != 0
	if reservedType || unknownFlags || callerAssignedIdentity {
		return os.ErrInvalid
	}
	return nil
}
