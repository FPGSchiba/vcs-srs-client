package profile

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeRoundTrip(t *testing.T) {
	src := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "Fleet Op",
		Radios:        []Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}},
		Layout: Layout{
			Window: WindowSize{W: 540, H: 720},
			Blocks: []Block{{RadioID: 1, Variant: "horizontal"}},
		},
	}
	b, err := Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(b), `"frequency_khz": 118500`) {
		t.Fatalf("frequency must serialise as an integer, got:\n%s", b)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("FrequencyKHz = %d, want 118500", got.Radios[0].FrequencyKHz)
	}
	if got.Layout.Blocks[0].Variant != "horizontal" {
		t.Fatalf("block Variant = %q, want horizontal", got.Layout.Blocks[0].Variant)
	}
}

func TestDecodeIgnoresUnknownFields(t *testing.T) {
	in := []byte(`{"schema_version":1,"name":"x","overlay":{"anchor":"top-right"},
	  "radios":[{"id":1,"name":"a","frequency_khz":118500,"enabled":true,"enc":true,"key":4}]}`)
	d, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(d.Radios) != 1 || d.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("radios = %+v", d.Radios)
	}
}

func TestValidateRejectsNewerSchema(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion + 1, Name: "x"}
	if err := d.Validate(); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}

// Review Focus #1.
func TestValidateRejectsOutOfRangeFrequency(t *testing.T) {
	for _, tc := range []struct {
		name string
		khz  uint32
	}{{"zero", 0}, {"above 24-bit ceiling", MaxFrequencyKHz + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Document{
				SchemaVersion: SchemaVersion,
				Name:          "x",
				Radios:        []Radio{{ID: 1, Name: "Fleet", FrequencyKHz: tc.khz}},
			}
			err := d.Validate()
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), "Fleet") {
				t.Fatalf("error must name the radio, got: %v", err)
			}
		})
	}
}

// Review Focus #5.
func TestValidateRejectsDuplicateRadioIDs(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 1, Name: "b", FrequencyKHz: 122750},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("duplicate radio ids must be rejected: resolveTXTarget takes the first match, so which frequency transmits would depend on array order")
	}
}

func TestValidateRejectsEmptyName(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "  "}
	if err := d.Validate(); err == nil {
		t.Fatal("want error for blank name")
	}
}

func TestValidateAcceptsEmptyRadios(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "empty", Radios: []Radio{}}
	if err := d.Validate(); err != nil {
		t.Fatalf("an empty profile is legal, got: %v", err)
	}
}

func TestReconcileAppendsMissingBlockWithDefaultVariant(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 2, Name: "b", FrequencyKHz: 122750},
		},
		Layout: Layout{Blocks: []Block{{RadioID: 1, Variant: "narrow-v"}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 2 {
		t.Fatalf("blocks = %+v, want one appended for radio 2", d.Layout.Blocks)
	}
	if got := d.Layout.Blocks[0]; got.Variant != "narrow-v" {
		t.Fatalf("existing block's variant must be preserved, got %+v", got)
	}
	if got := d.Layout.Blocks[1]; got.RadioID != 2 || got.Variant != DefaultVariant {
		t.Fatalf("appended block = %+v, want radio 2 at %q", got, DefaultVariant)
	}
}

func TestReconcileFillsEmptyVariant(t *testing.T) {
	// A hand-edited profile, or one written before a descriptor existed.
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: ""}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0].Variant; got != DefaultVariant {
		t.Fatalf("Variant = %q, want %q — an empty variant is a missing field, not an error", got, DefaultVariant)
	}
}

func TestReconcilePreservesAnUnknownVariant(t *testing.T) {
	// Go does not know the registry. A variant this build has never heard of
	// must survive a load/save cycle so a profile written by a NEWER client is
	// not silently downgraded — the frontend falls back at render time.
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: "dial-round"}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0].Variant; got != "dial-round" {
		t.Fatalf("Variant = %q, want the unknown id preserved", got)
	}
}

func TestReconcileDropsOrphanBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: "vertical"}, {RadioID: 9, Variant: "vertical"}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 1 || d.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want orphan dropped", d.Layout.Blocks)
	}
}

func TestBlockRoundTripsVariant(t *testing.T) {
	src := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: true}},
		Layout: Layout{
			Window: WindowSize{W: 540, H: 720},
			Blocks: []Block{{RadioID: 1, Variant: "narrow-h"}},
		},
	}
	b, err := Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(b), `"variant": "narrow-h"`) {
		t.Fatalf("block must serialise its variant:\n%s", b)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Layout.Blocks[0].Variant != "narrow-h" {
		t.Fatalf("blocks = %+v", got.Layout.Blocks)
	}
}

func TestReconcileFillsZeroWindowSize(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "x"}
	d.Reconcile()
	if d.Layout.Window.W != DefaultWindowW || d.Layout.Window.H != DefaultWindowH {
		t.Fatalf("window = %+v, want the %dx%d default", d.Layout.Window, DefaultWindowW, DefaultWindowH)
	}
}
