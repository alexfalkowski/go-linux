//go:build linux

package test_test

import (
	"reflect"
	"syscall"
	"testing"

	"github.com/alexfalkowski/go-linux/epoll"
	"github.com/alexfalkowski/go-linux/eventfd"
	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mount"
	"github.com/alexfalkowski/go-linux/pidfd"
	"github.com/alexfalkowski/go-linux/timerfd"
	"github.com/stretchr/testify/require"
)

func TestResourcesKeepDescriptorAccessPrivate(t *testing.T) {
	for _, resource := range []struct {
		name  string
		owner any
	}{
		{name: "memory file", owner: &memfd.File{}},
		{name: "event counter", owner: &eventfd.Counter{}},
		{name: "timer", owner: &timerfd.Timer{}},
		{name: "process", owner: &pidfd.Process{}},
		{name: "filesystem configuration", owner: &mount.Config{}},
		{name: "mount", owner: &mount.Handle{}},
		{name: "readiness set", owner: &epoll.Poller{}},
		{name: "readiness registration", owner: &epoll.Registration{}},
	} {
		t.Run(resource.name, func(t *testing.T) {
			require.NotImplements(t, (*syscall.Conn)(nil), resource.owner)
			require.NotImplements(t, (*interface{ Fd() uintptr })(nil), resource.owner)

			typeOf := reflect.TypeOf(resource.owner).Elem()
			for field := range typeOf.Fields() {
				require.False(t, field.IsExported(), "resource must hide field %s", field.Name)
			}
		})
	}
}
