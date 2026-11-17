//go:build linux

package server

import (
	"bufio"
	"context"
	"net"
	"strings"
	"time"

	"github.com/alexfalkowski/go-linux/examples/redis/internal/resp"
)

func (server *Server) handleConnection(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	interrupt := context.AfterFunc(ctx, func() {
		_ = connection.Close()
	})
	defer interrupt()
	reader := bufio.NewReaderSize(connection, 1024)
	for {
		if err := connection.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return
		}
		arguments, err := resp.ReadCommand(reader)
		if err != nil {
			_ = resp.WriteResponse(connection, resp.Failure("invalid or oversized RESP2 command"))
			return
		}
		if err := resp.WriteResponse(connection, server.handler.Execute(arguments)); err != nil {
			return
		}
		if len(arguments) == 1 && strings.EqualFold(string(arguments[0]), "QUIT") {
			return
		}
	}
}
