package gameconstants

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
)

type marshalCode struct {
	Code   []byte
	Consts []any
	Names  []string
	Name   string
}

type CodeObject = marshalCode

type marshalString []byte

type marshalPairs struct {
	Values []marshalPair
}

type marshalPair struct {
	Key, Value any
}

type marshalParser struct {
	data     []byte
	position int
	strings  []string
}

// DecodePYC decrypts one encrypted WoWS Python 2.7 module and reads its code object.
func DecodePYC(data []byte) (decoded *marshalCode, err error) {
	var (
		outer                                           *marshalCode
		inner                                           *marshalCode
		stage3                                          *marshalCode
		outerValue, innerValue, stage3Value, finalValue any
		outerConsts, innerConsts                        []any
		key, encrypted, xored, transformed, payload     []byte
		swapMap                                         *marshalPairs
		swapValues                                      map[byte]byte
		encoded                                         []byte
		plain                                           []byte
		position, second                                int
	)

	if len(data) < 9 {
		err = fmt.Errorf("pyc file is too short")
		return
	}

	if outerValue, err = readMarshal(data[8:]); err != nil {
		return
	}

	if outer, err = requireCode(outerValue); err != nil {
		return
	}

	outerConsts = outer.Consts
	if len(outerConsts) == 0 || len(outer.Code) == 0 {
		err = fmt.Errorf("outer code object has no cipher data")
		return
	}

	if encrypted, err = requireBytes(outerConsts[len(outerConsts)-1]); err != nil {
		return
	}

	key = outer.Code
	xored = make([]byte, len(encrypted))
	for index, value := range encrypted {
		xored[index] = value ^ key[index%len(key)]
	}

	if encoded, err = decodeBase64(xored); err != nil {
		return
	}

	if plain, err = decompressZlib(encoded); err != nil {
		err = fmt.Errorf("stage 1: %w", err)
		return
	}

	if innerValue, err = readMarshal(plain); err != nil {
		return
	}

	if inner, err = requireCode(innerValue); err != nil {
		return
	}

	innerConsts = inner.Consts
	if len(innerConsts) <= 8 {
		err = fmt.Errorf("stage 2 code object has too few constants")
		return
	}

	if nested, ok := innerConsts[8].(*marshalCode); !ok || len(nested.Consts) <= 1 {
		err = fmt.Errorf("stage 2 swap map is missing")
		return
	} else {
		swapMap, _ = nested.Consts[1].(*marshalPairs)
		swapValues = make(map[byte]byte)
		if swapMap != nil {
			for _, pair := range swapMap.Values {
				keyValue, keyOK := pair.Key.(int64)
				mappedValue, valueOK := pair.Value.(int64)
				if keyOK && valueOK && keyValue >= 0 && keyValue <= 255 && mappedValue >= 0 && mappedValue <= 255 {
					swapValues[byte(keyValue)] = byte(mappedValue)
				}
			}
		} else if sequence, ok := nested.Consts[1].([]any); ok {
			for index, item := range sequence {
				if mappedValue, ok := item.(int64); ok && index >= 0 && index <= 255 && mappedValue >= 0 && mappedValue <= 255 {
					swapValues[byte(index)] = byte(mappedValue)
				}
			}
		} else {
			err = fmt.Errorf("stage 2 swap map has an unsupported type")
			return
		}
	}

	transformed = make([]byte, len(key))
	for index, value := range key {
		mapped := swapValues[value]
		mapped ^= 38
		mapped = (mapped & 126) | ((mapped >> 7) & 1) | ((mapped & 1) << 7)
		transformed[index] = mapped ^ 89
	}

	for left, right := 0, len(transformed)-1; left < right; left, right = left+1, right-1 {
		transformed[left], transformed[right] = transformed[right], transformed[left]
	}

	if stage3Value, err = readMarshal(transformed); err != nil {
		return
	}

	if stage3, err = requireCode(stage3Value); err != nil {
		return
	}

	position = bytes.Index(stage3.Code, []byte("<<<>>>"))
	if position < 0 {
		err = fmt.Errorf("stage 4 delimiter is missing")
		return
	}

	payload = stage3.Code[position+6:]
	if second = bytes.Index(payload, []byte("<<<>>>")); second >= 0 {
		payload = payload[:second]
	}

	xored = make([]byte, len(payload))

	for index := range payload {
		xored[index] = payload[len(payload)-index-1]
	}

	if encoded, err = decodeBase64(xored); err != nil {
		return
	}

	if plain, err = decompressZlib(encoded); err != nil {
		err = fmt.Errorf("stage 4: %w", err)
		return
	}

	if finalValue, err = readMarshal(plain); err != nil {
		return
	}

	decoded, err = requireCode(finalValue)
	return
}

// AssignedConstants reads simple global assignments from a decrypted code object.
func AssignedConstants(code *marshalCode) (values map[string]any) {
	values = make(map[string]any)
	collectAssignments(code, values)
	return
}

// AssignedConstantsRecursive includes simple assignments from nested code objects.
func AssignedConstantsRecursive(code *marshalCode) (values map[string]any) {
	values = make(map[string]any)
	collectAssignmentsRecursive(code, values)
	return
}

// EvaluatedAssignments evaluates simple tuple and list declarations in Python code.
func EvaluatedAssignments(code *marshalCode) (values map[string]any) {
	values = AssignedConstantsRecursive(code)
	evaluateAssignmentsRecursive(code, values)
	return
}

// StringTuples returns literal string tuples found in a module and its nested code.
func StringTuples(code *marshalCode) (values [][]string) {
	collectStringTuples(code, &values)
	return
}

// StringConstants returns literal strings found in a module and its nested code.
func StringConstants(code *marshalCode) (values []string) {
	collectStringConstants(code, &values)
	return
}

// CodeObjects returns nested Python functions and the module object itself.
func CodeObjects(code *marshalCode) (objects []*marshalCode) {
	collectCodeObjects(code, &objects)
	return
}

func collectCodeObjects(code *marshalCode, objects *[]*marshalCode) {
	*objects = append(*objects, code)
	for _, constant := range code.Consts {
		if childCode, ok := constant.(*marshalCode); ok {
			collectCodeObjects(childCode, objects)
		}
	}
}

func collectStringTuples(code *marshalCode, values *[][]string) {
	var (
		strings []string
	)

	for _, constant := range code.Consts {
		if sequence, ok := constant.([]any); ok && len(sequence) > 5 {
			strings = make([]string, len(sequence))
			for index, value := range sequence {
				if byteString, ok := value.(marshalString); ok {
					strings[index] = string(byteString)
				} else if stringValue, ok := value.(string); ok {
					strings[index] = stringValue
				} else {
					strings = nil
					break
				}
			}

			if strings != nil {
				*values = append(*values, strings)
			}
		}

		if child, ok := constant.(*marshalCode); ok {
			collectStringTuples(child, values)
		}
	}
}

func collectStringConstants(code *marshalCode, values *[]string) {
	for _, constant := range code.Consts {
		switch typed := constant.(type) {
		case marshalString:
			*values = append(*values, string(typed))
		case string:
			*values = append(*values, typed)
		case *marshalCode:
			collectStringConstants(typed, values)
		}
	}
}

// collectAssignments follows nested functions and records LOAD_CONST assignments.
func collectAssignments(code *marshalCode, values map[string]any) {
	var (
		position, opcode, argument int
		previousConst              int = -1
		value                      any
	)

	for position = 0; position < len(code.Code); {
		opcode = int(code.Code[position])
		position++
		argument = 0

		if opcode >= 90 {
			if position+2 > len(code.Code) {
				return
			}
			argument = int(binary.LittleEndian.Uint16(code.Code[position : position+2]))
			position += 2
		}

		if (opcode == 90 || opcode == 97) && previousConst >= 0 && argument < len(code.Names) {
			if previousConst < len(code.Consts) {
				value = marshalValue(code.Consts[previousConst])
				if value != nil {
					values[code.Names[argument]] = value
				}
			}
		}

		if opcode == 100 {
			previousConst = argument
		} else {
			previousConst = -1
		}
	}
}

func collectAssignmentsRecursive(code *marshalCode, values map[string]any) {
	collectAssignments(code, values)
	for _, item := range code.Consts {
		if child, ok := item.(*marshalCode); ok {
			collectAssignmentsRecursive(child, values)
		}
	}
}

func evaluateAssignmentsRecursive(code *marshalCode, values map[string]any) {
	var (
		position, opcode, argument int
		name                       string
		stack                      []any
		items                      []any
		item                       any
		value                      any
	)

	for position = 0; position < len(code.Code); {
		opcode = int(code.Code[position])
		position++
		argument = 0

		if opcode >= 90 {
			if position+2 > len(code.Code) {
				break
			}

			argument = int(binary.LittleEndian.Uint16(code.Code[position : position+2]))
			position += 2
		}

		switch opcode {
		case 100:
			if argument < len(code.Consts) {
				stack = append(stack, marshalValue(code.Consts[argument]))
			} else {
				stack = append(stack, nil)
			}
		case 101, 116:
			if argument < len(code.Names) {
				stack = append(stack, values[code.Names[argument]])
			} else {
				stack = append(stack, nil)
			}
		case 102, 103, 104:
			if argument > len(stack) {
				stack = nil
				continue
			}

			items = append(make([]any, 0, argument), stack[len(stack)-argument:]...)
			stack = stack[:len(stack)-argument]
			stack = append(stack, items)
		case 90, 97:
			if argument < len(code.Names) && len(stack) > 0 {
				name = code.Names[argument]
				value = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if value != nil {
					values[name] = value
				}
			} else {
				stack = nil
			}
		case 1:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = nil
		}
	}

	for _, item = range code.Consts {
		if child, ok := item.(*marshalCode); ok {
			evaluateAssignmentsRecursive(child, values)
		}
	}
}

// marshalValue converts supported Python marshal values into JSON-compatible Go values.
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

func decodeBase64(data []byte) (decoded []byte, err error) {
	var filtered []byte
	for _, value := range data {
		if value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '+' || value == '/' || value == '=' {
			filtered = append(filtered, value)
		}
	}

	filtered = bytes.TrimRight(filtered, "=")
	if remainder := len(filtered) % 4; remainder > 0 {
		filtered = append(filtered, bytes.Repeat([]byte("="), 4-remainder)...)
	}

	decoded, err = base64.StdEncoding.DecodeString(string(filtered))
	return
}

func decompressZlib(data []byte) (decoded []byte, err error) {
	var reader io.ReadCloser

	if reader, err = zlib.NewReader(bytes.NewReader(data)); err != nil {
		return
	}

	decoded, err = io.ReadAll(reader)
	err = joinReadError(err, reader.Close())
	return
}

func joinReadError(readErr, closeErr error) (err error) {
	if readErr != nil {
		err = readErr
	} else {
		err = closeErr
	}

	return
}
