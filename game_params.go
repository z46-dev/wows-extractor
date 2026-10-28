package wowsextractor

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nlpodyssey/gopickle/types"
	"github.com/z46-dev/wows-extractor/internal/gameparams"
)

type GameParams struct {
	Region  string
	entries map[string]json.RawMessage
	names   []string
}

// LoadGameParams decodes one region from the archived GameParams file for repeated queries.
func (archive *Archive) LoadGameParams(region string) (params *GameParams, err error) {
	var (
		data      []byte
		decoded   *types.Dict
		name      string
		converted any
		encoded   []byte
		ok        bool
	)

	if data, err = archive.ReadFile("content/GameParams.data"); err != nil {
		err = fmt.Errorf("read GameParams: %w", err)
		return
	}

	if decoded, err = gameparams.Region(data, region); err != nil {
		return
	}

	params = &GameParams{Region: region, entries: make(map[string]json.RawMessage, decoded.Len())}
	for _, entry := range *decoded {
		if name, ok = entry.Key.(string); !ok {
			err = fmt.Errorf("non-string GameParams identifier %T", entry.Key)
			return
		}

		if converted, err = gameparams.ToJSONValue(entry.Value); err != nil {
			err = fmt.Errorf("GameParams %s/%s: %w", region, name, err)
			return
		}

		if encoded, err = json.Marshal(converted); err != nil {
			return
		}

		params.entries[name] = encoded
		params.names = append(params.names, name)
	}

	sort.Strings(params.names)
	return
}

// Query lists parameter identifiers with the requested prefix.
func (params *GameParams) Query(prefix string) (names []string) {
	for position := sort.SearchStrings(params.names, prefix); position < len(params.names); position++ {
		if !strings.HasPrefix(params.names[position], prefix) {
			break
		}

		names = append(names, params.names[position])
	}

	return
}

// Get returns a copy of one parameter's JSON representation.
func (params *GameParams) Get(name string) (value json.RawMessage, err error) {
	var (
		stored json.RawMessage
		found  bool
	)

	if stored, found = params.entries[name]; !found {
		err = os.ErrNotExist
		return
	}

	value = append(json.RawMessage(nil), stored...)
	return
}
