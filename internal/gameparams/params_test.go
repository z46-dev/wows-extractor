package gameparams

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/nlpodyssey/gopickle/types"
)

// TestRegion checks pickle object reconstruction and JSON field conversion.
func TestRegion(t *testing.T) {
	var (
		data      []byte
		params    *types.Dict
		value     any
		found     bool
		converted any
		encoded   []byte
		err       error
	)

	if data, err = base64.StdEncoding.DecodeString("gR8QOQB6xcVJpUnFPIUFmRmpiG5C8SBRsC5C2s4akAyOQvy0vMzUgsqSzIA4IrW92QpTMnrANNkLa1kLGzRZCrkSSxJcA9y5i3MSixIDU3MT3ZOZCgM8PYOyAWCKZC2sZCxz8GBgYmCIZC2qYGuceA=="); err != nil {
		t.Fatal(err)
	}

	if params, err = Region(data, "NA"); err != nil {
		t.Fatal(err)
	}

	if params.Len() != 1 {
		t.Fatalf("params count = %d", params.Len())
	}

	if value, found = params.Get("SHIP"); !found {
		t.Fatal("SHIP parameter is missing")
	}

	if converted, err = ToJSONValue(value); err != nil {
		t.Fatal(err)
	}

	if encoded, err = json.Marshal(converted); err != nil {
		t.Fatal(err)
	}

	if string(encoded) != `{"id":42,"typeinfo":{"type":"Ship"}}` {
		t.Fatalf("converted parameter = %s", encoded)
	}
}
