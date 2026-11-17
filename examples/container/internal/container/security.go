//go:build linux

package container

import (
	"path/filepath"

	"github.com/alexfalkowski/go-linux/capability"
	"github.com/alexfalkowski/go-linux/landlock"
	"github.com/alexfalkowski/go-linux/seccomp"
	"golang.org/x/sys/unix"
)

// securityPolicy applies the example's fixed restrictions after mount setup.
// It is deliberately limited to the policy described in the walkthrough.
type securityPolicy struct {
	root string
}

func (policy securityPolicy) apply() error {
	read := landlock.ReadFile | landlock.ReadDir | landlock.Execute
	write := landlock.WriteFile | landlock.RemoveDir | landlock.RemoveFile | landlock.MakeDir | landlock.MakeReg | landlock.MakeSym | landlock.Refer | landlock.Truncate
	rules, err := landlock.New(landlock.Config{HandledFS: read | write})
	if err != nil {
		return err
	}
	defer rules.Close()
	if err := rules.AddPath(policy.root, read); err != nil {
		return err
	}
	if err := rules.AddPath(filepath.Join(policy.root, "tmp"), read|write); err != nil {
		return err
	}
	if err := dropCapabilities(); err != nil {
		return err
	}
	if err := rules.Restrict(landlock.Options{Synchronization: landlock.CallingThread, NoNewPrivileges: true}); err != nil {
		return err
	}
	return filterChild()
}

func dropCapabilities() error {
	if err := capability.ClearAmbient(); err != nil {
		return err
	}
	sets, err := capability.Current()
	if err != nil {
		return err
	}
	sets.Inheritable = 0
	if err := capability.SetCurrent(sets); err != nil {
		return err
	}
	for bit := capability.Set(1); bit <= capability.CheckpointRestore; bit <<= 1 {
		present, err := capability.Bounding(bit)
		if err != nil {
			return err
		}
		if present {
			if err := capability.DropBounding(bit); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterChild() error {
	architecture, err := seccomp.NativeArchitecture()
	if err != nil {
		return err
	}
	instructions := []seccomp.Instruction{{Code: seccomp.LoadWord, K: seccomp.SyscallOffset}}
	for _, number := range []uint32{
		unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT,
		unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT, unix.SYS_MOUNT_SETATTR,
		unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_PTRACE, unix.SYS_BPF,
	} {
		instructions = append(instructions,
			seccomp.Instruction{Code: seccomp.JumpEqual, K: number, Jf: 1},
			seccomp.Instruction{Code: seccomp.Return, K: uint32(seccomp.Errno) | uint32(unix.EPERM)})
	}
	instructions = append(instructions, seccomp.Instruction{Code: seccomp.Return, K: uint32(seccomp.Allow)})
	filter, err := seccomp.New(architecture, instructions)
	if err != nil {
		return err
	}
	return filter.Load(seccomp.Options{Synchronization: seccomp.CallingThread, NoNewPrivileges: true})
}
