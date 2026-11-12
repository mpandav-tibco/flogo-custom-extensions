package datapipeline

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// descriptor.json, which the Flogo designer reads, names the inputs and
// outputs the activity reads and sets, with the defaults newInput gives.
func TestDescriptorMatchesTheActivity(t *testing.T) {
	raw, err := os.ReadFile("descriptor.json")
	require.NoError(t, err)
	var d struct {
		Ref    string `json:"ref"`
		Inputs []struct {
			Name  string      `json:"name"`
			Value interface{} `json:"value"`
		} `json:"inputs"`
		Outputs []struct {
			Name string `json:"name"`
		} `json:"outputs"`
	}
	require.NoError(t, json.Unmarshal(raw, &d))
	assert.Equal(t, "github.com/mpandav-tibco/flogo-custom-extensions/activity/datapipeline", d.Ref)

	var inputs, outputs []string
	defaults := newInput().ToMap()
	for _, in := range d.Inputs {
		inputs = append(inputs, in.Name)
		if in.Value != nil {
			assert.EqualValues(t, defaults[in.Name], in.Value, "default of %s", in.Name)
		}
	}
	for _, out := range d.Outputs {
		outputs = append(outputs, out.Name)
	}
	assert.Equal(t, keys(activityMd.Input), sorted(inputs))
	assert.Equal(t, keys(activityMd.Output), sorted(outputs))
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	sort.Strings(s)
	return s
}
