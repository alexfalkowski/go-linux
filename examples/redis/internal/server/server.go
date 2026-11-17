//go:build linux

package server

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/alexfalkowski/go-linux/examples/redis/internal/command"
	"github.com/alexfalkowski/go-linux/examples/redis/internal/store"
	"github.com/alexfalkowski/go-linux/timerfd"
)

// New borrows cache and acquires the listener and sweep timer, cleaning up on failure.
func New(ctx context.Context, address string, cache *store.Cache, logger *log.Logger) (*Server, error) {
	var listen net.ListenConfig
	listener, err := listen.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	timer, err := timerfd.New(timerfd.Boottime)
	if err != nil {
		return nil, errors.Join(err, listener.Close())
	}
	if _, err := timer.SetAfter(time.Second, time.Second); err != nil {
		return nil, errors.Join(err, timer.Close(), listener.Close())
	}
	return &Server{
		listener: listener,
		timer:    timer,
		logger:   logger,
		handler:  command.New(cache, logger),
		cache:    cache,
	}, nil
}

// Server owns the listening socket, sweep timer, and lifetime of all clients.
type Server struct {
	cache    *store.Cache
	listener net.Listener
	timer    *timerfd.Timer
	logger   *log.Logger
	handler  *command.Handler
}

// Close interrupts accepting connections and reading timer expirations.
func (server *Server) Close() error {
	return errors.Join(server.listener.Close(), server.timer.Close())
}

// Serve accepts clients and joins every client and sweep worker on return.
func (server *Server) Serve(parent context.Context) error {
	server.logger.Printf("listening %s; keys=%d; RESP2 subset", server.listener.Addr(), server.cache.Count())
	ctx, cancel := context.WithCancelCause(parent)
	interrupt := context.AfterFunc(ctx, func() {
		_ = server.Close()
	})
	var workers sync.WaitGroup
	defer func() {
		cancel(nil)
		_ = server.Close()
		workers.Wait()
		interrupt()
	}()
	workers.Go(func() {
		server.sweep(ctx, cancel)
	})
	return server.acceptConnections(ctx, &workers)
}

func (server *Server) sweep(ctx context.Context, cancel context.CancelCauseFunc) {
	for {
		if _, err := server.timer.ReadExpirations(); err != nil {
			if ctx.Err() == nil {
				cancel(err)
			}
			return
		}
		server.cache.Sweep(time.Now().UnixMilli())
	}
}

func (server *Server) acceptConnections(ctx context.Context, workers *sync.WaitGroup) error {
	slots := make(chan struct{}, 32)
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			workers.Go(func() {
				defer func() {
					<-slots
				}()
				server.handleConnection(ctx, connection)
			})
		default:
			_ = connection.Close()
		}
	}
}
