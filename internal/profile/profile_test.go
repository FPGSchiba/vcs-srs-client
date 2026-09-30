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
			Blocks: []Block{{RadioID: 1, W: 516, H: 180}},
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
	if got.Layout.Blocks[0].W != 516 {
		t.Fatalf("block W = %d, want 516", got.Layout.Blocks[0].W)
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

func TestReconcileAppendsMissingBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 2, Name: "b", FrequencyKHz: 122750},
		},
		Layout: Layout{Blocks: []Block{{RadioID: 1, W: 300, H: 100}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 2 {
		t.Fatalf("blocks = %+v, want one appended for radio 2", d.Layout.Blocks)
	}
	if last := d.Layout.Blocks[1]; last.RadioID != 2 || last.W != DefaultBlockW || last.H != DefaultBlockH {
		t.Fatalf("appended block = %+v, want radio 2 at default size", last)
	}
}

func TestReconcileDropsOrphanBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, W: 300, H: 100}, {RadioID: 9, W: 300, H: 100}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 1 || d.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want orphan dropped", d.Layout.Blocks)
	}
}

// Review Focus #2.
func TestReconcileClampsDegenerateBlockSizes(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, W: 0, H: -40}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0]; got.W != MinBlockW || got.H != MinBlockH {
		t.Fatalf("block = %+v, want clamped to %dx%d -- an invisible block cannot be grabbed to resize it back",
			got, MinBlockW, MinBlockH)
	}
}

func TestReconcileFillsZeroWindowSize(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "x"}
	d.Reconcile()
	if d.Layout.Window.W != DefaultWindowW || d.Layout.Window.H != DefaultWindowH {
		t.Fatalf("window = %+v, want the %dx%d default", d.Layout.Window, DefaultWindowW, DefaultWindowH)
	}
}
