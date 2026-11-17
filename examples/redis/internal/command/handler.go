//go:build linux

package command

import (
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/alexfalkowski/go-linux/examples/redis/internal/resp"
	"github.com/alexfalkowski/go-linux/examples/redis/internal/store"
)

// New borrows the cache and logger for request execution.
func New(cache *store.Cache, logger *log.Logger) *Handler {
	return &Handler{cache: cache, logger: logger}
}

// Handler translates RESP commands into one serialized cache operation.
type Handler struct {
	cache  *store.Cache
	logger *log.Logger
}

// Execute validates a command and maps typed store results into RESP2 responses.
func (handler *Handler) Execute(arguments [][]byte) resp.Response {
	now := time.Now().UnixMilli()
	handler.cache.Sweep(now)
	command := strings.ToUpper(string(arguments[0]))
	if !validArity(command, len(arguments)) {
		return resp.Failure("unsupported command or arguments; use PING/SET/GET/DEL/EXPIRE/TTL/SAVE/QUIT")
	}
	switch command {
	case "PING":
		return ping(arguments)
	case "QUIT":
		return resp.Response{Kind: '+', Data: []byte("OK")}
	case "SET":
		return handler.set(arguments, now)
	case "GET":
		return handler.get(string(arguments[1]), now)
	case "DEL":
		return handler.delete(arguments[1:], now)
	case "EXPIRE":
		return handler.setExpiry(arguments, now)
	case "TTL":
		return handler.ttl(string(arguments[1]), now)
	case "SAVE":
		return handler.saveResponse()
	}
	return resp.Failure("unsupported command")
}

func (handler *Handler) set(arguments [][]byte, now int64) resp.Response {
	expires, err := expiration(arguments, now)
	if err != nil {
		return resp.Failure(err.Error())
	}
	if err := handler.cache.Set(string(arguments[1]), arguments[2], expires, now); err != nil {
		return resp.Failure(err.Error())
	}
	return resp.Response{Kind: '+', Data: []byte("OK")}
}

func (handler *Handler) get(key string, now int64) resp.Response {
	value, exists := handler.cache.Get(key, now)
	if !exists {
		return resp.Response{Kind: '$', Number: -1}
	}
	return resp.Response{Kind: '$', Data: value.Value}
}

func (handler *Handler) delete(arguments [][]byte, now int64) resp.Response {
	keys := make([]string, len(arguments))
	for index, argument := range arguments {
		keys[index] = string(argument)
	}
	return resp.Response{Kind: ':', Number: int64(handler.cache.Delete(keys, now))}
}

func (handler *Handler) setExpiry(arguments [][]byte, now int64) resp.Response {
	seconds, err := strconv.ParseInt(string(arguments[2]), 10, 64)
	if err != nil {
		return resp.Failure("EXPIRE requires integer seconds <= 31536000")
	}
	if seconds > store.MaxTTLSeconds {
		return resp.Failure("EXPIRE requires integer seconds <= 31536000")
	}
	var expires int64
	if seconds <= 0 {
		expires = now
	} else {
		expires = now + seconds*1000
	}
	if handler.cache.ExpireAt(string(arguments[1]), expires, now) {
		return resp.Response{Kind: ':', Number: 1}
	}
	return resp.Response{Kind: ':', Number: 0}
}

func (handler *Handler) ttl(key string, now int64) resp.Response {
	value, exists := handler.cache.Get(key, now)
	var ttl int64
	switch {
	case !exists:
		ttl = -2
	case value.Expires == 0:
		ttl = -1
	default:
		ttl = (value.Expires - now + 500) / 1000
	}
	return resp.Response{Kind: ':', Number: ttl}
}

func (handler *Handler) saveResponse() resp.Response {
	if err := handler.cache.Save(); err != nil {
		if errors.Is(err, store.ErrSnapshotsDisabled) {
			return resp.Failure(err.Error())
		}
		handler.logger.Printf("snapshot: %v", err)
		return resp.Failure("snapshot failed; see server stderr")
	}
	return resp.Response{Kind: '+', Data: []byte("OK")}
}

func ping(arguments [][]byte) resp.Response {
	if len(arguments) == 1 {
		return resp.Response{Kind: '+', Data: []byte("PONG")}
	}
	return resp.Response{Kind: '$', Data: arguments[1]}
}

func validArity(command string, count int) bool {
	switch command {
	case "PING":
		return count == 1 || count == 2
	case "SET":
		return count == 3 || count == 5
	case "GET", "TTL":
		return count == 2
	case "DEL":
		return count >= 2
	case "EXPIRE":
		return count == 3
	case "SAVE", "QUIT":
		return count == 1
	default:
		return false
	}
}

func expiration(arguments [][]byte, now int64) (int64, error) {
	if len(arguments) == 3 {
		return 0, nil
	}
	seconds, err := strconv.ParseInt(string(arguments[4]), 10, 64)
	if err != nil {
		return 0, errors.New("EX requires integer seconds")
	}
	validExpiry := strings.EqualFold(string(arguments[3]), "EX") && seconds > 0 && seconds <= store.MaxTTLSeconds
	if !validExpiry {
		return 0, errors.New("use EX seconds between 1 and 31536000")
	}
	return now + seconds*1000, nil
}
