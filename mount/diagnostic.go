//go:build linux

package mount

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// Diagnostic is one kernel filesystem-context record. Severity is the kernel's
// leading byte (normally 'e', 'w', or 'i'); unknown severities are preserved.
// Message omits the severity separator and one trailing newline. Unframed
// records have zero Severity and retain their complete bytes in Message.
type Diagnostic struct {
	// Message omits the severity prefix and one trailing newline from framed
	// records. For unframed records, it preserves the complete record bytes.
	Message string

	// Severity is the kernel's leading byte, normally 'e', 'w', or 'i'.
	// Unknown severities are preserved; unframed records have zero Severity.
	Severity byte
}

// Error preserves a failed configuration operation and its pending kernel
// diagnostics. Diagnostics can include earlier warnings and need not be present
// for every failure. Err is the primary operation error; DiagnosticError is a
// separate retrieval failure, possibly after partial Diagnostics. Neither owns
// a resource. Callers retain and must close their existing Config.
type Error struct {
	// Err is the failed configuration operation's error, returned by Unwrap.
	Err error

	// DiagnosticError is a separate failure while retrieving diagnostics.
	// It is nil when retrieval succeeds and is not exposed through Unwrap.
	DiagnosticError error

	// Diagnostics contains records drained after the operation failed,
	// possibly including earlier warnings. It may be empty or, when
	// DiagnosticError is non-nil, contain only the records retrieved so far.
	Diagnostics []Diagnostic
}

// Error includes the primary error, diagnostic messages, and retrieval failure
// when present. Use the fields for structured inspection, not string parsing.
func (e *Error) Error() string {
	var text strings.Builder
	text.WriteString(e.Err.Error())
	for _, record := range e.Diagnostics {
		text.WriteString("; ")
		text.WriteString(record.Message)
	}
	if e.DiagnosticError != nil {
		text.WriteString("; diagnostics: ")
		text.WriteString(e.DiagnosticError.Error())
	}
	return text.String()
}

// Unwrap exposes only the primary error so errors.Is/As never mistake a
// diagnostic retrieval errno for the failed operation's errno.
func (e *Error) Unwrap() error {
	return e.Err
}

func readDiagnostics(fd int) ([]Diagnostic, error) {
	// Linux 7.0 logfc uses kasprintf without a fixed message-size bound.
	// fscontext_read retains an oversized record on EMSGSIZE, permitting growth.
	// https://github.com/torvalds/linux/blob/v7.0/fs/fs_context.c
	// https://github.com/torvalds/linux/blob/v7.0/fs/fsopen.c
	const limit = 1 << 20
	buffer := make([]byte, 4096)
	var records []Diagnostic
	for {
		n, err := unix.Read(fd, buffer)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.ENODATA):
			return records, nil
		case errors.Is(err, unix.EMSGSIZE) && len(buffer) < limit:
			buffer = make([]byte, len(buffer)*2)
			continue
		case err != nil:
			return records, err
		case n == 0:
			return records, nil
		}
		record := Diagnostic{Message: string(buffer[:n])}
		if n >= 2 && buffer[1] == ' ' {
			record.Severity = buffer[0]
			record.Message = strings.TrimSuffix(string(buffer[2:n]), "\n")
		}
		records = append(records, record)
	}
}
