//go:build linux

package test

import "syscall"

// ErrArgumentListTooLong preserves Linux E2BIG for exact error assertions.
const ErrArgumentListTooLong = syscall.E2BIG

// ErrPermissionDenied preserves Linux EACCES for exact error assertions.
const ErrPermissionDenied = syscall.EACCES

// ErrAddressNotAvailable preserves Linux EADDRNOTAVAIL for exact error assertions.
const ErrAddressNotAvailable = syscall.EADDRNOTAVAIL

// ErrAddressInUse preserves Linux EADDRINUSE for exact error assertions.
const ErrAddressInUse = syscall.EADDRINUSE

// ErrTryAgain preserves Linux EAGAIN for exact error assertions.
const ErrTryAgain = syscall.EAGAIN

// ErrWouldBlock preserves Linux EWOULDBLOCK for nonblocking lock contention.
const ErrWouldBlock = syscall.EWOULDBLOCK

// ErrBadDescriptor preserves Linux EBADF for exact error assertions.
const ErrBadDescriptor = syscall.EBADF

// ErrBusy preserves Linux EBUSY for exact error assertions.
const ErrBusy = syscall.EBUSY

// ErrCanceled preserves Linux ECANCELED for exact error assertions.
const ErrCanceled = syscall.ECANCELED

// ErrNoChild preserves Linux ECHILD for exact error assertions.
const ErrNoChild = syscall.ECHILD

// ErrConnectionRefused preserves Linux ECONNREFUSED for exact error assertions.
const ErrConnectionRefused = syscall.ECONNREFUSED

// ErrDeadlock preserves Linux EDEADLK for record-lock wait assertions.
const ErrDeadlock = syscall.EDEADLK

// ErrExists preserves Linux EEXIST for exact error assertions.
const ErrExists = syscall.EEXIST

// ErrFileTooLarge preserves Linux EFBIG for exact error assertions.
const ErrFileTooLarge = syscall.EFBIG

// ErrIllegalSeek preserves Linux ESPIPE for exact error assertions.
const ErrIllegalSeek = syscall.ESPIPE

// ErrInvalidArgument preserves Linux EINVAL for exact error assertions.
const ErrInvalidArgument = syscall.EINVAL

// ErrSymlinkLoop preserves Linux ELOOP for exact error assertions.
const ErrSymlinkLoop = syscall.ELOOP

// ErrTooManyOpenFiles preserves Linux EMFILE for exact error assertions.
const ErrTooManyOpenFiles = syscall.EMFILE

// ErrMessageTooLarge preserves Linux EMSGSIZE for exact error assertions.
const ErrMessageTooLarge = syscall.EMSGSIZE

// ErrNoData preserves Linux ENODATA for exact error assertions.
const ErrNoData = syscall.ENODATA

// ErrNoDevice preserves Linux ENODEV for exact error assertions.
const ErrNoDevice = syscall.ENODEV

// ErrNotExist preserves Linux ENOENT for exact error assertions.
const ErrNotExist = syscall.ENOENT

// ErrNoMemory preserves Linux ENOMEM for exact error assertions.
const ErrNoMemory = syscall.ENOMEM

// ErrNoSpace preserves Linux ENOSPC for exact error assertions.
const ErrNoSpace = syscall.ENOSPC

// ErrNoBufferSpace preserves Linux ENOBUFS for receive-queue loss assertions.
const ErrNoBufferSpace = syscall.ENOBUFS

// ErrNotDirectory preserves Linux ENOTDIR for exact error assertions.
const ErrNotDirectory = syscall.ENOTDIR

// ErrIsDirectory preserves Linux EISDIR for exact error assertions.
const ErrIsDirectory = syscall.EISDIR

// ErrNotSupported preserves Linux EOPNOTSUPP for exact error assertions.
const ErrNotSupported = syscall.EOPNOTSUPP

// ErrOverflow preserves Linux EOVERFLOW for exact error assertions.
const ErrOverflow = syscall.EOVERFLOW

// ErrOperationNotPermitted preserves Linux EPERM for exact error assertions.
const ErrOperationNotPermitted = syscall.EPERM

// ErrProtocol preserves Linux EPROTO for exact error assertions.
const ErrProtocol = syscall.EPROTO

// ErrRange preserves Linux ERANGE for exact error assertions.
const ErrRange = syscall.ERANGE

// ErrReadOnlyFilesystem preserves Linux EROFS for exact error assertions.
const ErrReadOnlyFilesystem = syscall.EROFS

// ErrNoProcess preserves Linux ESRCH for exact error assertions.
const ErrNoProcess = syscall.ESRCH

// ErrCrossDevice preserves Linux EXDEV for exact error assertions.
const ErrCrossDevice = syscall.EXDEV

// ErrTooManyUsers preserves Linux EUSERS for time-namespace thread restrictions.
const ErrTooManyUsers = syscall.EUSERS
