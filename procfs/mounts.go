//go:build linux

package procfs

import "strings"

// MountInfo describes the selected mountinfo fields. Propagation/other optional
// fields and mountstats are deliberately outside this initial API.
type MountInfo struct {
	// Options maps per-mount option names to values; bare options have empty values.
	Options map[string]string
	// SuperOptions maps superblock option names to values with the same convention.
	SuperOptions map[string]string
	// Root is the decoded root within the mounted filesystem.
	Root string
	// MountPoint is the decoded path relative to the inspected process's root.
	MountPoint string
	// Filesystem is the Linux filesystem type, such as proc or tmpfs.
	Filesystem string
	// Source is the decoded filesystem source, not necessarily a usable pathname.
	Source string
	// ID is the mount ID, which may be reused after an unmount.
	ID int
	// ParentID identifies the parent mount, possibly outside the visible root.
	ParentID int
	// Device identifies the filesystem's device as decimal major/minor numbers.
	Device Device
}

// Mounts reads [proc_pid_mountinfo(5)] in file order using the upstream full-file
// reader, avoiding its older 1-MiB-capped process method. Linux's \040, \011,
// \012 and \134 pathname escapes are decoded once. Unknown escape sequences
// and all option names/values are retained. Each call returns independent maps;
// mount IDs and paths can become stale immediately.
//
// [proc_pid_mountinfo(5)]: https://man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html
func (p *Process) Mounts() ([]MountInfo, error) {
	path, err := p.path("mountinfo")
	if err != nil {
		return nil, err
	}

	native, err := p.fs.fs.GetProcMounts(p.PID())
	if err != nil {
		return nil, readError(path, err)
	}

	mounts := make([]MountInfo, 0, len(native))
	for _, mount := range native {
		device, err := parseDevice(path, mount.MajorMinorVer, 10)
		if err != nil {
			return nil, err
		}

		if mount.MountID <= 0 || mount.ParentID < 0 {
			return nil, parseError(path)
		}

		mounts = append(mounts, MountInfo{
			ID: mount.MountID, ParentID: mount.ParentID, Device: device,
			Root: mountEscapes.Replace(mount.Root), MountPoint: mountEscapes.Replace(mount.MountPoint),
			Filesystem: mount.FSType, Source: mountEscapes.Replace(mount.Source),
			Options: mount.Options, SuperOptions: mount.SuperOptions,
		})
	}
	return mounts, nil
}

var mountEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
