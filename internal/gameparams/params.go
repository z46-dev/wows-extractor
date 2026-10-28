package gameparams

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"

	"github.com/nlpodyssey/gopickle/pickle"
	"github.com/nlpodyssey/gopickle/types"
)

type pythonClass struct{}

type pythonObject struct {
	fields map[string]any
}

// PyNew constructs a GameParams object without executing Python code.
func (pythonClass) PyNew(_ ...any) (value any, err error) {
	value = &pythonObject{fields: make(map[string]any)}
	return
}

// Call handles the older OBJ pickle opcode.
func (class pythonClass) Call(args ...any) (value any, err error) {
	value, err = class.PyNew(args...)
	return
}

// PySetState stores a GameParams object's serialized state.
func (object *pythonObject) PySetState(state any) (err error) {
	var (
		dict *types.Dict
		ok   bool
	)

	if dict, ok = state.(*types.Dict); !ok {
		err = fmt.Errorf("unexpected GameParams object state %T", state)
		return
	}

	for _, entry := range *dict {
		var key string
		if key, ok = entry.Key.(string); !ok {
			err = fmt.Errorf("non-string GameParams field %T", entry.Key)
			return
		}

		object.fields[key] = entry.Value
	}

	return
}

// Regions decodes every region in a reversed, zlib-compressed GameParams file.
func Regions(data []byte) (regions map[string]*types.Dict, err error) {
	var (
		reader io.ReadCloser
		root   any
		dict   *types.Dict
		ok     bool
	)

	for left, right := 0, len(data)-1; left < right; left, right = left+1, right-1 {
		data[left], data[right] = data[right], data[left]
	}

	if reader, err = zlib.NewReader(bytes.NewReader(data)); err != nil {
		return
	}

	defer reader.Close()

	var unpickler pickle.Unpickler = pickle.NewUnpickler(reader)
	unpickler.FindClass = func(module, name string) (class any, classErr error) {
		if module != "GameParams" {
			classErr = fmt.Errorf("unsupported pickle class %s.%s", module, name)
			return
		}

		class = pythonClass{}
		return
	}

	if root, err = unpickler.Load(); err != nil {
		return
	}

	if dict, ok = root.(*types.Dict); !ok {
		err = fmt.Errorf("unexpected GameParams root %T", root)
		return
	}

	regions = make(map[string]*types.Dict, dict.Len())
	for _, entry := range *dict {
		var (
			name   string
			params *types.Dict
		)
		if name, ok = entry.Key.(string); !ok {
			err = fmt.Errorf("unexpected GameParams region name %T", entry.Key)
			return
		}
		if params, ok = entry.Value.(*types.Dict); !ok {
			err = fmt.Errorf("unexpected GameParams region %s: %T", name, entry.Value)
			return
		}
		regions[name] = params
	}

	return
}

// Region selects one decoded GameParams region.
func Region(data []byte, region string) (params *types.Dict, err error) {
	var (
		regions map[string]*types.Dict
		found   bool
	)

	if regions, err = Regions(data); err != nil {
		return
	}
	if params, found = regions[region]; !found {
		err = fmt.Errorf("GameParams region %q is missing", region)
		return
	}

	return
}

// ToJSONValue converts a pickle value to the JSON shape used by wowsunpack.
func ToJSONValue(value any) (converted any, err error) {
	converted, err = convert(value, make(map[any]bool))
	return
}

// convert recursively translates pickle containers and breaks recursive back edges.
func convert(value any, visiting map[any]bool) (converted any, err error) {
	var (
		object map[string]any
		array  []any
		item   any
		key    string
		valid  bool
	)

	switch value := value.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		converted = value
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			err = fmt.Errorf("non-finite GameParams number")
			return
		}

		converted = value
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			err = fmt.Errorf("non-finite GameParams number")
			return
		}

		converted = value
	case *big.Int:
		converted = json.Number(value.String())
	case []byte:
		var raw []byte = value
		array = make([]any, len(raw))
		for position, number := range raw {
			array[position] = number
		}

		converted = array
	case *pythonObject:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)
		object = make(map[string]any, len(value.fields))
		for field, fieldValue := range value.fields {
			if item, err = convert(fieldValue, visiting); err != nil {
				return
			}

			object[field] = item
		}

		converted = object
	case *types.Dict:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)
		object = make(map[string]any, value.Len())
		for _, entry := range *value {
			if key, valid = jsonKey(entry.Key); !valid {
				continue
			}

			if item, err = convert(entry.Value, visiting); err != nil {
				return
			}

			object[key] = item
		}

		converted = object
	case *types.List:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)

		var list *types.List = value
		array = make([]any, 0, len(*list))
		for _, element := range *list {
			if item, err = convert(element, visiting); err != nil {
				return
			}

			array = append(array, item)
		}

		converted = array
	case *types.Tuple:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)

		var tuple *types.Tuple = value
		array = make([]any, 0, len(*tuple))
		for _, element := range *tuple {
			if item, err = convert(element, visiting); err != nil {
				return
			}

			array = append(array, item)
		}

		converted = array
	case *types.Set:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)
		array = make([]any, 0, len(*value))
		for element := range *value {
			if item, err = convert(element, visiting); err != nil {
				return
			}

			array = append(array, item)
		}

		converted = array
	case *types.FrozenSet:
		if visiting[value] {
			return
		}

		visiting[value] = true
		defer delete(visiting, value)
		array = make([]any, 0, len(*value))
		for element := range *value {
			if item, err = convert(element, visiting); err != nil {
				return
			}

			array = append(array, item)
		}

		converted = array
	default:
		err = fmt.Errorf("unsupported GameParams value %T", value)
	}

	return
}

// jsonKey preserves string and numeric dictionary keys.
func jsonKey(value any) (key string, valid bool) {
	switch value := value.(type) {
	case string:
		key = value
	case int:
		key = strconv.Itoa(value)
	case int64:
		key = strconv.FormatInt(value, 10)
	case uint64:
		key = strconv.FormatUint(value, 10)
	case float64:
		key = strconv.FormatFloat(value, 'g', -1, 64)
	default:
		return
	}

	valid = true
	return
}
