//go:build linux

package procfs

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// MapPermissions describes the fixed rwx and private/shared positions in maps.
// Values returned by Maps contain exactly one of MapPrivate and MapShared.
type MapPermissions uint8

// MapRead reports a readable mapping.
const MapRead MapPermissions = 1 << 0

// MapWrite reports a writable mapping.
const MapWrite MapPermissions = 1 << 1

// MapExecute reports an executable mapping.
const MapExecute MapPermissions = 1 << 2

// MapPrivate reports a private copy-on-write mapping.
const MapPrivate MapPermissions = 1 << 3

// MapShared reports a shared mapping.
const MapShared MapPermissions = 1 << 4

// Device identifies a Linux device without exposing a native dev_t encoding.
type Device struct {
	// Major identifies the device class or driver.
	Major uint32
	// Minor identifies the device instance within that major number.
	Minor uint32
}

// Mapping describes one textual /proc/PID/maps region. Addresses are numbers
// in the inspected process, not pointers that the caller may dereference.
type Mapping struct {
	// Name is the textual pathname or bracketed label, or empty for anonymous
	// mappings. Internal and trailing whitespace and Linux escapes are retained.
	Name string
	// Start is the inclusive virtual start address.
	Start uint64
	// End is the exclusive virtual end address.
	End uint64
	// Offset is the file offset in bytes.
	Offset uint64
	// Inode is zero when no backing inode is associated with the mapping.
	Inode uint64
	// Device identifies the backing filesystem's device.
	Device Device
	// Permissions reports the native mapping access and sharing mode.
	Permissions MapPermissions
}

// Maps reads [proc_pid_maps(5)] in file order, preserving pathname whitespace.
// Newlines in filenames remain the ambiguous Linux textual escape \012; the
// package does not resolve names, remove " (deleted)", or follow map_files links.
// Reads are not atomic with mapping changes. Malformed or over-1-MiB records
// match ErrParse, and read errors discard every accumulated mapping.
//
// [proc_pid_maps(5)]: https://man7.org/linux/man-pages/man5/proc_pid_maps.5.html
func (p *Process) Maps() ([]Mapping, error) {
	path, err := p.path("maps")
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Upstream v0.23.0 joins pathname fields and ignores Scanner.Err. Keep
	// this reader local until its public parser preserves both contracts.
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	scanner.Split(mappingLines)
	mappings := make([]Mapping, 0)
	for scanner.Scan() {
		mapping, err := parseMapping(path, scanner.Text())
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	if err := scanner.Err(); err != nil {
		return nil, readError(path, err)
	}
	return mappings, nil
}

func parseMapping(path, line string) (Mapping, error) {
	fields, name, ok := mappingFields(line)
	if !ok {
		return Mapping{}, parseError(path)
	}

	startText, endText, found := strings.Cut(fields[0], "-")
	if !found {
		return Mapping{}, parseError(path)
	}

	start, err := parseUint(path, startText, 16, 64)
	if err != nil {
		return Mapping{}, err
	}

	end, err := parseUint(path, endText, 16, 64)
	if err != nil {
		return Mapping{}, err
	}
	if start >= end {
		return Mapping{}, parseError(path)
	}

	permissions, err := mappingPermissions(path, fields[1])
	if err != nil {
		return Mapping{}, err
	}

	offset, err := parseUint(path, fields[2], 16, 64)
	if err != nil {
		return Mapping{}, err
	}

	device, err := parseDevice(path, fields[3], 16)
	if err != nil {
		return Mapping{}, err
	}

	inode, err := parseUint(path, fields[4], 10, 64)
	if err != nil {
		return Mapping{}, err
	}
	return Mapping{Name: name, Start: start, End: end, Offset: offset, Inode: inode, Device: device, Permissions: permissions}, nil
}

func mappingFields(line string) ([5]string, string, bool) {
	var fields [5]string
	for i := range fields {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			return fields, "", false
		}
		end := strings.IndexAny(line, " \t")
		if end < 0 {
			fields[i], line = line, ""
		} else {
			fields[i], line = line[:end], line[end:]
		}
	}
	return fields, strings.TrimLeft(line, " \t"), true
}

// Unlike bufio.ScanLines, mappingLines retains a trailing carriage return:
// Linux permits that byte in a pathname and does not escape it in maps.
func mappingLines(data []byte, atEOF bool) (int, []byte, error) {
	if end := bytes.IndexByte(data, '\n'); end >= 0 {
		return end + 1, data[:end], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func mappingPermissions(path, text string) (MapPermissions, error) {
	if len(text) != 4 {
		return 0, parseError(path)
	}

	var permissions MapPermissions
	for i := range 3 {
		if text[i] == "rwx"[i] {
			permissions |= MapRead << i
		} else if text[i] != '-' {
			return 0, parseError(path)
		}
	}

	switch text[3] {
	case 'p':
		permissions |= MapPrivate
	case 's':
		permissions |= MapShared
	default:
		return 0, parseError(path)
	}
	return permissions, nil
}

func parseDevice(path, text string, base int) (Device, error) {
	majorText, minorText, found := strings.Cut(text, ":")
	if !found {
		return Device{}, parseError(path)
	}

	major, err := parseUint(path, majorText, base, 32)
	if err != nil {
		return Device{}, err
	}

	minor, err := parseUint(path, minorText, base, 32)
	if err != nil {
		return Device{}, err
	}

	return Device{Major: uint32(major), Minor: uint32(minor)}, nil //nolint:gosec // Both components were parsed with 32-bit bounds.
}
