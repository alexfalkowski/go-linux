//go:build linux

package seccomp_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/seccomp"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Reserve Go's startup thread so teardown observations use worker threads.
	test.LockThread()
	os.Exit(m.Run())
}

func TestNewRejectsMismatchedArchitectures(t *testing.T) {
	native, err := seccomp.NativeArchitecture()
	require.NoError(t, err)
	for _, architecture := range []seccomp.Architecture{0, seccomp.AMD64, seccomp.ARM64, 1} {
		if architecture == native {
			continue
		}
		filter, err := seccomp.New(architecture, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})
		require.ErrorIs(t, err, os.ErrInvalid, "architecture=%#x", architecture)
		require.Nil(t, filter)
	}
}

func TestNewRejectsInvalidPrograms(t *testing.T) {
	native, err := seccomp.NativeArchitecture()
	require.NoError(t, err)
	allow := seccomp.Instruction{Code: seccomp.Return, K: uint32(seccomp.Allow)}
	for name, instructions := range map[string][]seccomp.Instruction{
		"empty":                nil,
		"too long":             make([]seccomp.Instruction, seccomp.MaxInstructions+1),
		"missing final return": {{Code: seccomp.LoadConstant}},
		"unknown opcode":       {{Code: 0xffff}, allow},
		"unaligned load":       {{Code: seccomp.LoadWord, K: 1}, allow},
		"past data":            {{Code: seccomp.LoadWord, K: 64}, allow},
		"huge offset":          {{Code: seccomp.LoadWord, K: ^uint32(0)}, allow},
		"past true jump":       {{Code: seccomp.JumpEqual, Jt: 1}, allow},
		"past false jump":      {{Code: seccomp.JumpEqual, Jf: 1}, allow},
		"past absolute jump":   {{Code: seccomp.Jump, K: 1}, allow},
		"huge absolute jump":   {{Code: seccomp.Jump, K: ^uint32(0)}, allow},
		"ignored true field":   {{Code: seccomp.LoadConstant, Jt: 1}, allow},
		"ignored false field":  {{Code: seccomp.Return, Jf: 1, K: uint32(seccomp.Allow)}},
		"unknown action":       {{Code: seccomp.Return, K: 0x12340000}},
		"user notification":    {{Code: seccomp.Return, K: 0x7fc00000}},
		"ignored return data":  {{Code: seccomp.Return, K: uint32(seccomp.Allow) | 1}},
	} {
		t.Run(name, func(t *testing.T) {
			filter, err := seccomp.New(native, instructions)
			require.ErrorIs(t, err, os.ErrInvalid)
			require.Nil(t, filter)
		})
	}
}

func TestNewCopiesProgramStorage(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	native, err := seccomp.NativeArchitecture()
	require.NoError(t, err)
	instructions := []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}}
	filter, err := seccomp.New(native, instructions)
	require.NoError(t, err)
	instructions[0].K = uint32(seccomp.Errno) | uint32(test.ErrPermissionDenied)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))

	group, err := test.ProcessGroup(0)
	require.NoError(t, err, "changing the source must not change the constructed filter")
	require.Positive(t, group)
}

func TestMaximumProgramLoads(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	native, err := seccomp.NativeArchitecture()
	require.NoError(t, err)
	instructions := make([]seccomp.Instruction, seccomp.MaxInstructions)
	for index := range instructions {
		instructions[index] = seccomp.Instruction{Code: seccomp.LoadConstant, K: uint32(index)}
	}
	instructions[len(instructions)-1] = seccomp.Instruction{Code: seccomp.Return, K: uint32(seccomp.Allow)}
	filter, err := seccomp.New(native, instructions)
	require.NoError(t, err)

	require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))
}

func TestLoadRejectsInvalidScopeBeforeChangingState(t *testing.T) {
	if !test.SeccompProcess(t) {
		return
	}
	test.LockThread()
	filter := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})
	before := test.NoNewPrivileges(t)
	filters := test.SeccompFilterCount(t)

	for _, scope := range []seccomp.Synchronization{0, 3, 255} {
		err := filter.Load(seccomp.Options{Synchronization: scope, NoNewPrivileges: true})
		require.ErrorIs(t, err, os.ErrInvalid, "scope=%d", scope)
	}

	require.Equal(t, before, test.NoNewPrivileges(t), "invalid scope must not set no-new-privileges")
	require.Equal(t, filters, test.SeccompFilterCount(t), "invalid scope must not attach a filter")
}

func TestLoadRejectsNilAndZeroFilters(t *testing.T) {
	for _, filter := range []*seccomp.Filter{nil, {}} {
		require.ErrorIs(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}), os.ErrInvalid)
	}
}

func TestActionAvailability(t *testing.T) {
	for _, action := range []seccomp.Action{seccomp.KillProcess, seccomp.KillThread, seccomp.Trap, seccomp.Errno, seccomp.Trace, seccomp.Log, seccomp.Allow} {
		available, err := seccomp.ActionAvailable(action)
		require.NoError(t, err, "action=%#x", action)
		require.True(t, available, "baseline action=%#x", action)
	}
}

func TestActionAvailabilityRejectsDataAndUnknownActions(t *testing.T) {
	for _, action := range []seccomp.Action{seccomp.Errno | 1, seccomp.Allow | 1, 0x12340000, 0x7fc00000} {
		available, err := seccomp.ActionAvailable(action)
		require.ErrorIs(t, err, os.ErrInvalid, "action=%#x", action)
		require.False(t, available)
	}
}

func TestGuardRejectsX32(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("x32 is an x86-64 alternate syscall ABI")
	}
	const marker = "seccomp-x32"
	if os.Args[len(os.Args)-1] == marker {
		test.LockThread()
		filter := test.SeccompFilter(t, []seccomp.Instruction{{Code: seccomp.Return, K: uint32(seccomp.Allow)}})
		require.NoError(t, filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true}))
		_, err := os.Stdout.WriteString("loaded\n")
		require.NoError(t, err)
		test.InvokeX32(t)
		return
	}
	ctx, command := test.Command(t, t.Name(), "--", marker)

	output, err := command.CombinedOutput()

	require.NoError(t, ctx.Err())
	require.Equal(t, "loaded\n", string(output))
	test.RequireSeccompTermination(t, err)
}
