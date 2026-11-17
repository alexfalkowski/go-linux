//go:build linux

package cgroup

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Limit is a nonnegative byte or task count, or Max. Its text representation is
// the cgroup decimal/max format. Values below Max are invalid. Finite values
// above int64 are outside this API's supported range.
type Limit int64

// Max selects the cgroup "max" value: no limit at this level. Ancestor limits
// still apply. Zero is a finite zero limit, never an unlimited default.
const Max Limit = -1

// MarshalText encodes the decimal/max cgroup representation without a newline.
// Invalid negative values return an error matching os.ErrInvalid.
func (l Limit) MarshalText() ([]byte, error) {
	switch {
	case l == Max:
		return []byte("max"), nil
	case l < 0:
		return nil, os.ErrInvalid
	default:
		return []byte(strconv.FormatInt(int64(l), 10)), nil
	}
}

// UnmarshalText decodes one decimal/max value with optional surrounding space.
// Signs, overflow, and extra fields match os.ErrInvalid and leave l unchanged.
func (l *Limit) UnmarshalText(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "max" {
		*l = Max
		return nil
	}

	value, err := decimal(text)
	if err != nil || value > 1<<63-1 {
		return os.ErrInvalid
	}

	*l = Limit(value)
	return nil
}

// UnlimitedQuota selects "max" for CPUBandwidth.Quota. It is a sentinel, not
// a negative interval; a zero quota remains invalid.
const UnlimitedQuota time.Duration = -1

// CPUBandwidth describes cpu.max for fair-class tasks. The zero value is invalid.
// Only whole microseconds are accepted; values are never silently rounded.
type CPUBandwidth struct {
	// Quota is CPU time available per Period, or UnlimitedQuota. A finite quota
	// must be at least one millisecond and can exceed Period for multiple CPUs.
	Quota time.Duration
	// Period is the accounting interval, from one millisecond to one second.
	Period time.Duration
}

// MarshalText encodes the complete "quota period" cpu.max record in microseconds.
// Invalid durations match os.ErrInvalid. Both fields are always written.
func (b CPUBandwidth) MarshalText() ([]byte, error) {
	periodOutOfRange := b.Period < time.Millisecond || b.Period > time.Second
	fractionalPeriod := b.Period%time.Microsecond != 0
	if periodOutOfRange || fractionalPeriod {
		return nil, os.ErrInvalid
	}

	var quota string
	if b.Quota == UnlimitedQuota {
		quota = "max"
	} else {
		quotaTooSmall := b.Quota < time.Millisecond
		fractionalQuota := b.Quota%time.Microsecond != 0
		if quotaTooSmall || fractionalQuota {
			return nil, os.ErrInvalid
		}
		quota = strconv.FormatInt(b.Quota.Microseconds(), 10)
	}
	return []byte(quota + " " + strconv.FormatInt(b.Period.Microseconds(), 10)), nil
}

// UnmarshalText decodes a complete cpu.max record. Missing/extra fields,
// overflow, or invalid intervals match os.ErrInvalid and leave b unchanged.
func (b *CPUBandwidth) UnmarshalText(data []byte) error {
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return os.ErrInvalid
	}

	period, err := decimal(fields[1])
	maxPeriod := uint64(time.Second / time.Microsecond)
	if err != nil || period > maxPeriod {
		return os.ErrInvalid
	}

	var quota time.Duration
	if fields[0] == "max" {
		quota = UnlimitedQuota
	} else {
		value, err := decimal(fields[0])
		maxQuota := uint64((1<<63 - 1) / time.Microsecond)
		if err != nil || value > maxQuota {
			return os.ErrInvalid
		}
		quota = time.Duration(value) * time.Microsecond
	}

	value := CPUBandwidth{Quota: quota, Period: time.Duration(period) * time.Microsecond}
	if _, err := value.MarshalText(); err != nil {
		return err
	}

	*b = value
	return nil
}

// ParseCounters decodes a cgroup flat-keyed unsigned counter snapshot, such as
// cpu.stat, memory.events, or pids.events. Unknown keys are retained. Empty
// input, duplicate keys, nondecimal/overflowing values, and records with other
// than two fields match os.ErrInvalid and return no partial result. Blank lines
// are ignored; keys and their units retain the named Linux file's meaning.
func ParseCounters(data []byte) (map[string]uint64, error) {
	values := make(map[string]uint64)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, os.ErrInvalid
		}
		if _, exists := values[fields[0]]; exists {
			return nil, os.ErrInvalid
		}

		value, err := decimal(fields[1])
		if err != nil {
			return nil, err
		}
		values[fields[0]] = value
	}
	if len(values) == 0 {
		return nil, os.ErrInvalid
	}
	return values, nil
}

// ParseEvents decodes cgroup.events. populated and frozen must each be present
// exactly once with a 0 or 1 value. Other unsigned fields are preserved. Invalid
// input matches os.ErrInvalid and returns a zero Event. ParseCounters supplies
// the flat-keyed parsing rules.
func ParseEvents(data []byte) (Event, error) {
	values, err := ParseCounters(data)
	if err != nil {
		return Event{}, err
	}

	populated, hasPopulated := values["populated"]
	frozen, hasFrozen := values["frozen"]
	missingState := !hasPopulated || !hasFrozen
	invalidState := populated > 1 || frozen > 1
	if missingState || invalidState {
		return Event{}, os.ErrInvalid
	}

	delete(values, "populated")
	delete(values, "frozen")
	return Event{Populated: populated == 1, Frozen: frozen == 1, Other: values}, nil
}

func decimal(text string) (uint64, error) {
	empty := text == ""
	containsNonDigit := strings.IndexFunc(text, func(r rune) bool {
		return r < '0' || r > '9'
	}) >= 0
	if empty || containsNonDigit {
		return 0, os.ErrInvalid
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, os.ErrInvalid
	}
	return value, nil
}
