//go:build linux

package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/alexfalkowski/go-linux/examples/redis/internal/resp"
)

// New selects a server address and borrows output without opening a connection.
func New(address string, output io.Writer) *Client {
	return &Client{address: address, output: output}
}

// Client sends one command and renders its result to a borrowed writer.
type Client struct {
	output  io.Writer
	address string
}

// Run sends literal arguments, defaulting to PING, with a five-second bound.
func (client *Client) Run(parent context.Context, arguments []string) error {
	if len(arguments) == 0 {
		arguments = []string{"PING"}
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", client.address)
	if err != nil {
		return err
	}
	defer connection.Close()
	interrupt := context.AfterFunc(ctx, func() {
		_ = connection.Close()
	})
	defer interrupt()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if err := resp.WriteCommand(connection, arguments); err != nil {
		return err
	}
	reply, err := resp.ReadResponse(bufio.NewReaderSize(connection, 1024))
	if err != nil {
		return err
	}
	return printResponse(client.output, reply)
}

func printResponse(output io.Writer, reply resp.Response) error {
	var err error
	switch {
	case reply.Kind == '-':
		return errors.New(string(reply.Data))
	case reply.Kind == ':':
		_, err = fmt.Fprintln(output, reply.Number)
	case reply.Kind == '$' && reply.Number == -1:
		_, err = fmt.Fprintln(output, "(nil)")
	default:
		_, err = fmt.Fprintf(output, "%s\n", reply.Data)
	}
	return err
}
