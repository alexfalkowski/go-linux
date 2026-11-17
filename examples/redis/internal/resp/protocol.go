//go:build linux

package resp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	maxArguments = 16
	maxBulk      = 64 << 10
	maxRequest   = 128 << 10
)

// Response is the bounded RESP2 response representation.
type Response struct {
	// Data owns string payload bytes for simple, error, and bulk responses.
	Data []byte
	// Number is an integer value, or -1 for a null bulk response.
	Number int64
	// Kind is a RESP2 tag: '+', '-', ':', or '$'.
	Kind byte
}

func readNumber(reader *bufio.Reader, prefix byte) (int64, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return 0, err
	}
	validFrame := len(line) >= 4 && line[0] == prefix && line[len(line)-2] == '\r'
	if !validFrame {
		return 0, errors.New("invalid RESP frame")
	}
	return strconv.ParseInt(string(line[1:len(line)-2]), 10, 64)
}

func readBulk(reader *bufio.Reader, length int64) ([]byte, error) {
	if length < 0 || length > maxBulk {
		return nil, errors.New("bulk string exceeds the supported limit")
	}
	data := make([]byte, int(length)+2)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	if data[len(data)-2] != '\r' || data[len(data)-1] != '\n' {
		return nil, errors.New("invalid bulk terminator")
	}
	return data[:len(data)-2], nil
}

// ReadCommand decodes one bounded array of bulk strings with owned argument bytes.
func ReadCommand(reader *bufio.Reader) ([][]byte, error) {
	count, err := readNumber(reader, '*')
	if err != nil {
		return nil, err
	}
	if count < 1 || count > maxArguments {
		return nil, errors.New("unsupported argument count")
	}
	arguments := make([][]byte, int(count))
	var total int64
	for index := range arguments {
		length, err := readNumber(reader, '$')
		if err != nil {
			return nil, err
		}
		if length < 0 || length > maxBulk || total > maxRequest-length {
			return nil, errors.New("command exceeds the supported limit")
		}
		data, err := readBulk(reader, length)
		if err != nil {
			return nil, err
		}
		arguments[index] = data
		total += length
	}
	return arguments, nil
}

// WriteCommand encodes literal arguments as one bounded RESP2 command.
func WriteCommand(writer io.Writer, arguments []string) error {
	if len(arguments) == 0 || len(arguments) > maxArguments {
		return errors.New("unsupported argument count")
	}
	if _, err := fmt.Fprintf(writer, "*%d\r\n", len(arguments)); err != nil {
		return err
	}
	var total int
	for _, argument := range arguments {
		if len(argument) > maxBulk || total > maxRequest-len(argument) {
			return errors.New("command exceeds the supported limit")
		}
		total += len(argument)
		if _, err := fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(argument), argument); err != nil {
			return err
		}
	}
	return nil
}

// ReadResponse decodes one simple, error, integer, or bounded bulk response.
func ReadResponse(reader *bufio.Reader) (Response, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return Response{}, err
	}
	if len(line) < 3 || line[len(line)-2] != '\r' {
		return Response{}, errors.New("invalid response")
	}
	kind, text := line[0], line[1:len(line)-2]
	switch kind {
	case '+', '-':
		return Response{Kind: kind, Data: append([]byte(nil), text...)}, nil
	case ':', '$':
		return numericResponse(reader, kind, text)
	default:
		return Response{}, errors.New("unsupported response type")
	}
}

func numericResponse(reader *bufio.Reader, kind byte, text []byte) (Response, error) {
	number, err := strconv.ParseInt(string(text), 10, 64)
	if err != nil {
		return Response{}, err
	}
	if kind == ':' || number == -1 {
		return Response{Kind: kind, Number: number}, nil
	}
	data, err := readBulk(reader, number)
	if err != nil {
		return Response{}, err
	}
	return Response{Kind: kind, Data: data}, nil
}

// WriteResponse writes one complete response without closing the writer.
func WriteResponse(writer io.Writer, reply Response) error {
	var err error
	switch reply.Kind {
	case '+', '-':
		_, err = fmt.Fprintf(writer, "%c%s\r\n", reply.Kind, reply.Data)
	case ':':
		_, err = fmt.Fprintf(writer, ":%d\r\n", reply.Number)
	case '$':
		if reply.Number == -1 {
			_, err = io.WriteString(writer, "$-1\r\n")
		} else {
			_, err = fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(reply.Data), reply.Data)
		}
	default:
		return errors.New("unsupported response type")
	}
	return err
}

// Failure adds the ERR prefix to a caller-supplied single-line message.
func Failure(message string) Response {
	return Response{Kind: '-', Data: []byte("ERR " + message)}
}
