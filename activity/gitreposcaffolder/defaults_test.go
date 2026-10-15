package gitreposcaffolder

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/project-flogo/core/support/test"
)

// The runtime seeds unmapped inputs from activityMd, so its defaults must match
// the designer defaults in descriptor.json (a mismatch once made new repos public).
func TestRuntimeDefaultsMatchDescriptor(t *testing.T) {
	var d struct {
		Inputs []struct {
			Name  string      `json:"name"`
			Value interface{} `json:"value"`
		} `json:"inputs"`
	}
	b, err := os.ReadFile("descriptor.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, in := range d.Inputs {
		if in.Value == nil {
			continue
		}
		tv, ok := activityMd.Input[in.Name]
		if !ok {
			t.Errorf("descriptor input %q missing from metadata", in.Name)
			continue
		}
		want, _ := json.Marshal(in.Value)
		got, _ := json.Marshal(tv.Value())
		if string(want) != string(got) {
			t.Errorf("default for %q: descriptor %s, runtime %s", in.Name, want, got)
		}
		checked++
	}
	if checked < 15 {
		t.Fatalf("only %d defaults checked", checked)
	}
}

// Unmapped inputs must behave like the documented defaults when going through a Flogo context.
func TestUnmappedInputsUseDefaults(t *testing.T) {
	tc := test.NewActivityContext(activityMd)
	tc.SetInput("templateRepoURL", "https://github.com/o/t")
	in := newInput()
	*in = Input{}
	if err := tc.GetInputObject(in); err != nil {
		t.Fatal(err)
	}
	if !in.NewRepoPrivate || !in.RenamePaths || !in.CleanTargetFolder || !in.FailIfRepoExists ||
		in.WriteStrategy != WriteStrategyAuto || in.TargetBranch != "main" || in.PushRetries != 3 {
		t.Fatalf("unmapped inputs lost their defaults: %+v", in)
	}
}
