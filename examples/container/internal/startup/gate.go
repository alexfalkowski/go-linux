//go:build linux

package startup

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// New acquires both ends of a pipe used to gate helper startup.
func New() (*Gate, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &Gate{reader: reader, writer: writer}, nil
}

// Gate owns the parent references to the helper startup pipe.
type Gate struct {
	reader *os.File
	writer *os.File
}

// Reader borrows the helper's stdin stream until Release or Close.
func (gate *Gate) Reader() io.Reader { return gate.reader }

// Close releases both parent references, including after partial startup.
func (gate *Gate) Close() error { return errors.Join(gate.reader.Close(), gate.writer.Close()) }

// Release allows the helper to proceed after cgroup membership is configured.
func (gate *Gate) Release() error {
	if err := gate.reader.Close(); err != nil {
		return err
	}
	if _, err := io.WriteString(gate.writer, "G"); err != nil {
		return err
	}
	return gate.writer.Close()
}

// Wait reads the release byte; EOF without a release aborts helper startup.
func Wait(input io.Reader) error {
	var gate [1]byte
	if _, err := io.ReadFull(input, gate[:]); err != nil {
		return fmt.Errorf("parent startup gate: %w", err)
	}
	if gate[0] != 'G' {
		return errors.New("invalid startup gate")
	}
	return nil
}
