package configuration

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

const (
	nbtEnd      = 0
	nbtByte     = 1
	nbtString   = 8
	nbtList     = 9
	nbtCompound = 10
	nbtInt      = 3
	nbtLong     = 4
	nbtFloat    = 5
	nbtDouble   = 6
)

var ErrNBTTooLarge = errors.New("NBT document exceeds limit")

type nbtField struct {
	name string
	data []byte
}

type Compound struct {
	fields []nbtField
}

func NewCompound() *Compound {
	return &Compound{fields: make([]nbtField, 0, 8)}
}

func (c *Compound) Byte(name string, value byte) *Compound {
	return c.add(nbtByte, name, []byte{value})
}

func (c *Compound) Int(name string, value int32) *Compound {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], uint32(value))
	return c.add(nbtInt, name, data[:])
}

func (c *Compound) Long(name string, value int64) *Compound {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(value))
	return c.add(nbtLong, name, data[:])
}

func (c *Compound) Float(name string, value float32) *Compound {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], float32Bits(value))
	return c.add(nbtFloat, name, data[:])
}

func (c *Compound) Double(name string, value float64) *Compound {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], float64Bits(value))
	return c.add(nbtDouble, name, data[:])
}

func (c *Compound) String(name, value string) *Compound {
	return c.add(nbtString, name, appendStringPayload(value))
}

func (c *Compound) Compound(name string, value *Compound) *Compound {
	return c.add(nbtCompound, name, value.encodePayload())
}

func (c *Compound) ListOfCompounds(name string, values ...*Compound) *Compound {
	data := make([]byte, 5, 5)
	data[0] = nbtCompound
	binary.BigEndian.PutUint32(data[1:5], uint32(len(values)))
	for _, value := range values {
		data = append(data, value.encodePayload()...)
	}
	return c.add(nbtList, name, data)
}

func (c *Compound) add(kind byte, name string, data []byte) *Compound {
	c.fields = append(c.fields, nbtField{name: name, data: append([]byte(nil), data...)})
	// The tag kind is stored in the first byte of the field payload.
	c.fields[len(c.fields)-1].data = append([]byte{kind}, c.fields[len(c.fields)-1].data...)
	return c
}

func (c *Compound) Encode(maxSize int) ([]byte, error) {
	data := []byte{nbtCompound}
	data = append(data, c.encodePayload()...)
	if len(data) > maxSize {
		return nil, ErrNBTTooLarge
	}
	return data, nil
}

func (c *Compound) encodePayload() []byte {
	data := make([]byte, 0, 64)
	for _, field := range c.fields {
		kind := field.data[0]
		data = append(data, kind)
		data = append(data, appendStringPayload(field.name)...)
		data = append(data, field.data[1:]...)
	}
	return append(data, nbtEnd)
}

func RegistryDataPayload(registry *Compound) ([]byte, error) {
	nbt, err := registry.Encode(2 << 20)
	if err != nil {
		return nil, err
	}
	payload := codec.AppendVarInt(nil, 0x05)
	return append(payload, nbt...), nil
}

func appendStringPayload(value string) []byte {
	data := make([]byte, 2+len(value))
	binary.BigEndian.PutUint16(data[:2], uint16(len(value)))
	copy(data[2:], value)
	return data
}

func float32Bits(value float32) uint32 { return math.Float32bits(value) }
func float64Bits(value float64) uint64 { return math.Float64bits(value) }
