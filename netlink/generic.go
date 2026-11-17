//go:build linux

package netlink

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"

	"github.com/alexfalkowski/go-linux/socket"
	"golang.org/x/sys/unix"
)

// ControllerID is the stable Generic Netlink controller family ID (GENL_ID_CTRL).
const ControllerID uint16 = unix.GENL_ID_CTRL

// GetFamilyCommand queries the Generic Netlink controller (CTRL_CMD_GETFAMILY).
const GetFamilyCommand uint8 = unix.CTRL_CMD_GETFAMILY

// NewFamilyCommand identifies a discovered or registered family (CTRL_CMD_NEWFAMILY).
const NewFamilyCommand uint8 = unix.CTRL_CMD_NEWFAMILY

// DeleteFamilyCommand identifies an unregistered family (CTRL_CMD_DELFAMILY).
const DeleteFamilyCommand uint8 = unix.CTRL_CMD_DELFAMILY

// FamilyIDAttribute carries the controller family ID as a native uint16 (CTRL_ATTR_FAMILY_ID).
const FamilyIDAttribute AttributeType = unix.CTRL_ATTR_FAMILY_ID

// FamilyNameAttribute carries a NUL-terminated family name (CTRL_ATTR_FAMILY_NAME).
const FamilyNameAttribute AttributeType = unix.CTRL_ATTR_FAMILY_NAME

// FamilyVersionAttribute carries the family version as a native uint32 (CTRL_ATTR_VERSION).
const FamilyVersionAttribute AttributeType = unix.CTRL_ATTR_VERSION

// FamilyHeaderSizeAttribute carries the family header size as a native uint32 (CTRL_ATTR_HDRSIZE).
const FamilyHeaderSizeAttribute AttributeType = unix.CTRL_ATTR_HDRSIZE

// FamilyMaxAttribute carries the maximum family attribute ID as a native uint32 (CTRL_ATTR_MAXATTR).
const FamilyMaxAttribute AttributeType = unix.CTRL_ATTR_MAXATTR

// FamilyOperationsAttribute carries nested controller operation records (CTRL_ATTR_OPS).
const FamilyOperationsAttribute AttributeType = unix.CTRL_ATTR_OPS

// FamilyGroupsAttribute carries nested controller multicast groups (CTRL_ATTR_MCAST_GROUPS).
const FamilyGroupsAttribute AttributeType = unix.CTRL_ATTR_MCAST_GROUPS

// GroupNameAttribute carries a NUL-terminated multicast group name (CTRL_ATTR_MCAST_GRP_NAME).
const GroupNameAttribute AttributeType = unix.CTRL_ATTR_MCAST_GRP_NAME

// GroupIDAttribute carries the numeric multicast group ID as a native uint32 (CTRL_ATTR_MCAST_GRP_ID).
const GroupIDAttribute AttributeType = unix.CTRL_ATTR_MCAST_GRP_ID

// ParseFamily decodes a controller NEWFAMILY/DELFAMILY message, applying the
// controller's width/string/nesting policy while retaining unknown attributes.
// ID and Name are required; duplicate known attributes and malformed groups
// match ErrMalformed. Legacy controller nests need not have NLA_F_NESTED set.
// All attribute bytes borrow message.Data.
func ParseFamily(message Message) (Family, error) {
	if message.Header.Type != unix.GENL_ID_CTRL {
		return Family{}, ErrMalformed
	}

	generic, err := unmarshalGenericMessage(message.Data)
	if err != nil {
		return Family{}, ErrMalformed
	}
	reservedData := generic.Reserved != 0
	wrongCommand := generic.Command != unix.CTRL_CMD_NEWFAMILY && generic.Command != unix.CTRL_CMD_DELFAMILY
	if reservedData || wrongCommand {
		return Family{}, ErrMalformed
	}

	attributes, err := UnmarshalAttributes(generic.Data)
	if err != nil {
		return Family{}, err
	}

	family := Family{Attributes: attributes}
	if err := family.decodeAttributes(); err != nil {
		return Family{}, err
	}
	return family, nil
}

// GenericMessage is a Linux Generic Netlink message: genlmsghdr (command and
// version) followed by a family-specific payload. It is carried in Message.Data;
// Message supplies the outer nlmsghdr. "Generic" names the Linux protocol.
type GenericMessage struct {
	// Data follows genlmsghdr, including any family-specific fixed header.
	Data []byte
	// Reserved preserves the reserved 16-bit header field; send zero.
	Reserved uint16
	// Command is the family-specific Generic Netlink command.
	Command uint8
	// Version is the family protocol version, not a kernel release number.
	Version uint8
}

// MarshalBinary encodes genlmsghdr with a copy of Data. Reserved must be zero
// for requests, but is encoded verbatim to support record round trips.
func (m GenericMessage) MarshalBinary() ([]byte, error) {
	if uint64(len(m.Data)) > 1<<32-1-unix.NLMSG_HDRLEN-unix.GENL_HDRLEN {
		return nil, os.ErrInvalid
	}

	data := make([]byte, unix.GENL_HDRLEN+len(m.Data))
	data[0], data[1] = m.Command, m.Version
	binary.NativeEndian.PutUint16(data[2:], m.Reserved)
	copy(data[4:], m.Data)
	return data, nil
}

// UnmarshalBinary decodes genlmsghdr and copies the family payload, implementing
// encoding.BinaryUnmarshaler. Inputs below four bytes match ErrMalformed and
// leave the receiver unchanged. The receiver does not retain the input buffer.
func (m *GenericMessage) UnmarshalBinary(data []byte) error {
	decoded, err := unmarshalGenericMessage(data)
	if err != nil {
		return err
	}
	decoded.Data = bytes.Clone(decoded.Data)
	*m = decoded
	return nil
}

// Family is one Generic Netlink controller record. IDs are discovered at runtime
// and can be reused after unregistering a family; no result is cached globally.
type Family struct {
	// Name is the controller's NUL-free family name (1..15 bytes).
	Name string
	// Groups lists discovered multicast group names and numeric IDs.
	Groups []Group
	// Attributes retains all controller attributes, including operations/policies
	// and unknown IDs. This is discovery data, not an enforced family schema.
	Attributes []Attribute
	// Version is the controller's 32-bit family version.
	Version uint32
	// HeaderSize counts family-specific fixed header bytes after genlmsghdr.
	HeaderSize uint32
	// MaxAttribute is the highest attribute ID advertised by the family.
	MaxAttribute uint32
	// ID is the dynamically assigned nlmsg_type used for family requests.
	ID uint16
}

// Group is one Generic Netlink multicast group advertised by the controller.
type Group struct {
	// Name is a NUL-free group name within its family.
	Name string
	// ID is a positive numeric membership ID, not a bitmask.
	ID uint32
}

// LookupFamily queries the Generic Netlink controller by a 1..15-byte name.
// Invalid names/protocols match os.ErrInvalid; missing families preserve ENOENT.
// Unknown names may trigger Linux module autoloading; use known names when only
// observation is intended. Results are bounded by Conn's Options and deadlines.
func (c *Conn) LookupFamily(name string) (Family, error) {
	wrongProtocol := c.protocol != socket.Generic
	invalidName := name == "" || len(name) >= unix.GENL_NAMSIZ || strings.ContainsRune(name, 0)
	if wrongProtocol || invalidName {
		return Family{}, os.ErrInvalid
	}

	attributes, err := MarshalAttributes([]Attribute{{Type: unix.CTRL_ATTR_FAMILY_NAME, Data: append([]byte(name), 0)}})
	if err != nil {
		return Family{}, err
	}

	data := append([]byte{unix.CTRL_CMD_GETFAMILY, 2, 0, 0}, attributes...)

	response, err := c.Execute(Message{Header: Header{Type: unix.GENL_ID_CTRL}, Data: data})
	if err != nil {
		return Family{}, err
	}
	if len(response.Messages) != 1 {
		return Family{}, ErrUnexpectedReply
	}

	family, err := ParseFamily(response.Messages[0])
	if err != nil {
		return Family{}, err
	}
	if family.Name != name {
		return Family{}, ErrUnexpectedReply
	}
	return family, nil
}

// Families dumps registered Generic Netlink families. Invalid protocols match
// os.ErrInvalid. Failure, including an interrupted dump or malformed record,
// returns nil rather than a partial family list. Use Dump for partial wire data.
func (c *Conn) Families() ([]Family, error) {
	if c.protocol != socket.Generic {
		return nil, os.ErrInvalid
	}

	data, err := (GenericMessage{Command: unix.CTRL_CMD_GETFAMILY, Version: 2}).MarshalBinary()
	if err != nil {
		return nil, err
	}

	response, err := c.Dump(Message{Header: Header{Type: unix.GENL_ID_CTRL}, Data: data})
	if err != nil {
		return nil, err
	}

	families := make([]Family, 0, len(response.Messages))
	for _, message := range response.Messages {
		family, err := ParseFamily(message)
		if err != nil {
			return nil, err
		}
		families = append(families, family)
	}
	return families, nil
}

func (f *Family) decodeAttributes() error {
	seen := make(map[uint16]bool)
	for _, attribute := range f.Attributes {
		id := attribute.ID()
		if id < unix.CTRL_ATTR_FAMILY_ID || id > unix.CTRL_ATTR_MCAST_GROUPS {
			continue
		}
		if seen[id] || attribute.Type&NetworkByteOrder != 0 {
			return ErrMalformed
		}
		seen[id] = true
		if err := f.decodeAttribute(attribute); err != nil {
			return err
		}
	}

	reservedID := f.ID < unix.NLMSG_MIN_TYPE
	invalidName := f.Name == "" || len(f.Name) >= unix.GENL_NAMSIZ
	if reservedID || invalidName {
		return ErrMalformed
	}
	return nil
}

func (f *Family) decodeAttribute(attribute Attribute) error {
	var err error
	switch attribute.ID() {
	case unix.CTRL_ATTR_FAMILY_ID:
		f.ID, err = attribute.Uint16()
	case unix.CTRL_ATTR_FAMILY_NAME:
		f.Name, err = attribute.String()
	case unix.CTRL_ATTR_VERSION:
		f.Version, err = attribute.Uint32()
	case unix.CTRL_ATTR_HDRSIZE:
		f.HeaderSize, err = attribute.Uint32()
	case unix.CTRL_ATTR_MAXATTR:
		f.MaxAttribute, err = attribute.Uint32()
	case unix.CTRL_ATTR_MCAST_GROUPS:
		f.Groups, err = parseGroups(attribute.Data)
	}
	return err
}

func parseGroups(data []byte) ([]Group, error) {
	entries, err := UnmarshalAttributes(data)
	if err != nil {
		return nil, err
	}

	groups := make([]Group, 0, len(entries))
	for _, entry := range entries {
		group, err := parseGroup(entry.Data)
		if err != nil {
			return nil, err
		}
		missingID := group.ID == 0
		invalidName := group.Name == "" || len(group.Name) >= unix.GENL_NAMSIZ
		if missingID || invalidName {
			return nil, ErrMalformed
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func parseGroup(data []byte) (Group, error) {
	attributes, err := UnmarshalAttributes(data)
	if err != nil {
		return Group{}, err
	}

	var group Group
	seen := make(map[uint16]bool)
	for _, attribute := range attributes {
		id := attribute.ID()
		if id != unix.CTRL_ATTR_MCAST_GRP_NAME && id != unix.CTRL_ATTR_MCAST_GRP_ID {
			continue
		}

		if seen[id] || attribute.Type&NetworkByteOrder != 0 {
			return Group{}, ErrMalformed
		}

		seen[id] = true
		switch id {
		case unix.CTRL_ATTR_MCAST_GRP_NAME:
			group.Name, err = attribute.String()
		case unix.CTRL_ATTR_MCAST_GRP_ID:
			group.ID, err = attribute.Uint32()
		}
		if err != nil {
			return Group{}, err
		}
	}
	return group, nil
}

func unmarshalGenericMessage(data []byte) (GenericMessage, error) {
	if len(data) < unix.GENL_HDRLEN {
		return GenericMessage{}, ErrMalformed
	}
	return GenericMessage{Command: data[0], Version: data[1], Reserved: binary.NativeEndian.Uint16(data[2:]), Data: data[4:]}, nil
}
