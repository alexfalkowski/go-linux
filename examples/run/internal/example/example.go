//go:build linux

package example

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

// Selection is a validated example name and its literal application arguments.
type Selection struct {
	// Name is one of the launcher allowlisted application names.
	Name string
	// Arguments contains literal argv; redis-cli selections include the client flag.
	Arguments []string
}

// Parse reads the Make launcher's selection and JSON argv without shell evaluation.
func Parse() (Selection, error) {
	if len(os.Args) != 2 {
		return Selection{}, errors.New("use make example-top|example-watch|example-supervise|example-redis|example-redis-cli|example-container")
	}
	name := os.Args[1]
	switch name {
	case "top", "watch", "supervise", "redis", "redis-cli", "container":
	default:
		return Selection{}, errors.New("unknown example")
	}
	var arguments []string
	if raw := os.Getenv("GO_LINUX_EXAMPLE_ARGS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
			return Selection{}, fmt.Errorf("args must be a JSON array of strings, for example args='[\"-once\"]': %w", err)
		}
	}
	if name == "redis-cli" {
		return Selection{Name: "redis", Arguments: append([]string{"-client"}, arguments...)}, nil
	}
	return Selection{Name: name, Arguments: arguments}, nil
}

// ShutdownGrace allows managed applications to finish their configured child
// grace and reap the workload before the launcher escalates to KILL.
// Other applications receive five seconds. Very large durations saturate.
func (selected Selection) ShutdownGrace() (time.Duration, error) {
	const cleanup = 5 * time.Second
	var grace time.Duration
	switch selected.Name {
	case "supervise":
		grace = 2 * time.Second
	case "watch":
		grace = time.Second
	default:
		return cleanup, nil
	}
	argument, supplied := selected.graceArgument()
	if supplied {
		parsed, err := time.ParseDuration(argument)
		if err != nil {
			return 0, fmt.Errorf("grace: %w", err)
		}
		if parsed <= 0 {
			return 0, errors.New("grace must be positive")
		}
		grace = parsed
	}
	if grace > math.MaxInt64-cleanup {
		return math.MaxInt64, nil
	}
	return grace + cleanup, nil
}

func (selected Selection) graceArgument() (string, bool) {
	arguments := selected.Arguments
	var grace string
	var supplied bool
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if !strings.HasPrefix(argument, "-") {
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		switch name {
		case "", "h", "help":
			return grace, supplied
		case "initial":
			// Match flag's Boolean handling; other application flags consume a value.
			if selected.Name == "watch" {
				continue
			}
		}
		if !hasValue {
			if len(arguments) == 0 {
				break // The application's flag parser reports the missing value.
			}
			value, arguments = arguments[0], arguments[1:]
		}
		if name == "grace" {
			grace, supplied = value, true
		}
	}
	return grace, supplied
}
