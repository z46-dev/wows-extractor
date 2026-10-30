package gameconstants

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
)

func marshalValue(value any) (converted any) {
	var (
		index  int
		item   any
		values []any
		object map[string]any
		key    string
	)

	switch typed := value.(type) {
	case []any:
		values = make([]any, len(typed))
		for index, item = range typed {
			values[index] = marshalValue(item)
		}

		converted = values
	case marshalString:
		converted = string(typed)
	case *marshalPairs:
		object = make(map[string]any, len(typed.Values))
		for _, pair := range typed.Values {
			switch typedKey := pair.Key.(type) {
			case string:
				key = typedKey
			case int64:
				key = strconv.FormatInt(typedKey, 10)
			default:
				continue
			}

			object[key] = marshalValue(pair.Value)
		}

		converted = object
	case *marshalCode:
		converted = nil
	default:
		converted = value
	}

	return
}

func readMarshal(data []byte) (value any, err error) {
	var parser marshalParser

	parser = marshalParser{data: data}
	value, err = parser.readObject(0)
	return
}

func (parser *marshalParser) readObject(depth int) (value any, err error) {
	var (
		tag       byte
		length    int
		count     int
		integer   int32
		integer64 int64
		negative  bool
		items     []any
		pairs     *marshalPairs
		code      *marshalCode
		first     any
		second    any
		item      any
	)

	if depth > 128 {
		err = fmt.Errorf("marshal object nesting exceeds limit")
		return
	}

	if tag, err = parser.readByte(); err != nil {
		return
	}

	switch tag {
	case 'N':
		value = nil
	case 'T':
		value = true
	case 'F':
		value = false
	case 'i':
		if integer, err = parser.readInt32(); err == nil {
			value = int64(integer)
		}
	case 'I':
		if integer64, err = parser.readInt64(); err == nil {
			value = integer64
		}
	case 's', 't', 'u':
		if length, err = parser.readLength(); err != nil {
			return
		}

		var raw []byte
		if raw, err = parser.readBytes(length); err != nil {
			return
		}

		value = marshalString(raw)
		if tag == 't' {
			parser.strings = append(parser.strings, string(raw))
		}
	case 'R':
		if integer, err = parser.readInt32(); err != nil {
			return
		}

		if integer < 0 || int(integer) >= len(parser.strings) {
			err = fmt.Errorf("invalid marshal string reference %d", integer)
			return
		}

		value = parser.strings[integer]
	case 'g':
		var raw []byte
		if raw, err = parser.readBytes(8); err == nil {
			value = math.Float64frombits(binary.LittleEndian.Uint64(raw))
		}
	case 'f':
		if length, err = parser.readByteInt(); err != nil {
			return
		}

		var raw []byte
		if raw, err = parser.readBytes(length); err != nil {
			return
		}

		value, err = strconv.ParseFloat(string(raw), 64)
	case '(':
		if length, err = parser.readLength(); err != nil {
			return
		}

		items = make([]any, length)
		for index := range items {
			if items[index], err = parser.readObject(depth + 1); err != nil {
				return
			}
		}

		value = items
	case ')':
		if length, err = parser.readByteInt(); err != nil {
			return
		}

		items = make([]any, length)
		for index := range items {
			if items[index], err = parser.readObject(depth + 1); err != nil {
				return
			}
		}

		value = items
	case '[':
		if length, err = parser.readLength(); err != nil {
			return
		}

		items = make([]any, length)
		for index := range items {
			if items[index], err = parser.readObject(depth + 1); err != nil {
				return
			}
		}

		value = items
	case '{':
		pairs = &marshalPairs{}
		for {
			if parser.position >= len(parser.data) {
				err = io.ErrUnexpectedEOF
				return
			}

			if parser.data[parser.position] == '0' {
				parser.position++
				break
			}

			if first, err = parser.readObject(depth + 1); err != nil {
				return
			}

			if second, err = parser.readObject(depth + 1); err != nil {
				return
			}

			pairs.Values = append(pairs.Values, marshalPair{Key: first, Value: second})
		}

		value = pairs
	case 'l':
		if integer, err = parser.readInt32(); err != nil {
			return
		}

		count = int(integer)
		negative = count < 0
		if negative {
			count = -count
		}

		if count > 100000 {
			err = fmt.Errorf("unsupported marshal long with %d digits", count)
			return
		}

		bigValue := new(big.Int)
		for digit := 0; digit < count; digit++ {
			var raw []byte
			if raw, err = parser.readBytes(2); err != nil {
				return
			}

			bigValue.Add(bigValue, new(big.Int).Lsh(big.NewInt(int64(binary.LittleEndian.Uint16(raw))), uint(15*digit)))
		}

		if negative {
			bigValue.Neg(bigValue)
		}

		value = bigValue
	case 'c':
		code = &marshalCode{}
		for range 4 {
			if integer, err = parser.readInt32(); err != nil {
				return
			}
		}

		if first, err = parser.readObject(depth + 1); err != nil {
			return
		}

		if code.Code, err = requireBytes(first); err != nil {
			return
		}

		if second, err = parser.readObject(depth + 1); err != nil {
			return
		}

		code.Consts, _ = second.([]any)
		if item, err = parser.readObject(depth + 1); err != nil {
			return
		}

		code.Names, _ = stringsFromObject(item)
		for range 4 {
			if item, err = parser.readObject(depth + 1); err != nil {
				return
			}
		}

		if item, err = parser.readObject(depth + 1); err != nil {
			return
		}

		if byteString, ok := item.(marshalString); ok {
			code.Name = string(byteString)
		} else if name, ok := item.(string); ok {
			code.Name = name
		}

		if integer, err = parser.readInt32(); err != nil {
			return
		}

		if item, err = parser.readObject(depth + 1); err != nil {
			return
		}

		value = code
	default:
		err = fmt.Errorf("unsupported marshal type 0x%02x at offset %d", tag, parser.position-1)
	}

	return
}

func stringsFromObject(value any) (names []string, valid bool) {
	var (
		index int
		item  any
	)

	items, valid := value.([]any)
	if !valid {
		return
	}

	names = make([]string, len(items))
	for index, item = range items {
		if byteString, ok := item.(marshalString); ok {
			names[index] = string(byteString)
		} else if names[index], valid = item.(string); !valid {
			names = nil
			return
		}
	}

	return
}

func (parser *marshalParser) readLength() (value int, err error) {
	var integer int32

	if integer, err = parser.readInt32(); err != nil {
		return
	}

	if integer < 0 || integer > 100000000 {
		err = fmt.Errorf("invalid marshal length %d", integer)
		return
	}

	value = int(integer)
	return
}

func (parser *marshalParser) readByteInt() (value int, err error) {
	var raw byte

	if raw, err = parser.readByte(); err == nil {
		value = int(raw)
	}

	return
}

func (parser *marshalParser) readByte() (value byte, err error) {
	if parser.position >= len(parser.data) {
		err = io.ErrUnexpectedEOF
		return
	}

	value = parser.data[parser.position]
	parser.position++
	return
}

func (parser *marshalParser) readInt32() (value int32, err error) {
	var raw []byte

	if raw, err = parser.readBytes(4); err == nil {
		err = binary.Read(bytes.NewReader(raw), binary.LittleEndian, &value)
	}

	return
}

func (parser *marshalParser) readInt64() (value int64, err error) {
	var raw []byte

	if raw, err = parser.readBytes(8); err == nil {
		err = binary.Read(bytes.NewReader(raw), binary.LittleEndian, &value)
	}

	return
}

func (parser *marshalParser) readBytes(length int) (value []byte, err error) {
	if length < 0 || length > len(parser.data)-parser.position {
		err = io.ErrUnexpectedEOF
		return
	}

	value = parser.data[parser.position : parser.position+length]
	parser.position += length
	return
}

func requireCode(value any) (code *marshalCode, err error) {
	var valid bool

	if code, valid = value.(*marshalCode); !valid {
		err = fmt.Errorf("marshal object is not a code object")
	}

	return
}

func requireBytes(value any) (data []byte, err error) {
	var valid bool

	if byteString, ok := value.(marshalString); ok {
		data = []byte(byteString)
	} else if data, valid = value.([]byte); !valid {
		err = fmt.Errorf("marshal object is not a byte string")
	}

	return
}
