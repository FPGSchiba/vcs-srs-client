package keybinds

import "testing"

func TestStaticActionsCoverAllCategories(t *testing.T) {
	seen := map[Category]int{}
	for _, a := range StaticActions() {
		seen[a.Category]++
	}
	for _, c := range []Category{CatGlobal, CatChannel, CatStatus} {
		if seen[c] == 0 {
			t.Errorf("no static actions in category %v", c)
		}
	}
	if seen[CatPerRadio] != 0 {
		t.Error("per-radio actions must not be static")
	}
}

func TestPTTIsHold(t *testing.T) {
	want := map[ActionID]Kind{
		"global.ptt":          KindHold,
		"global.push_to_mute": KindHold,
		"global.mute_toggle":  KindPress,
		"status.afk":          KindPress,
	}
	got := map[ActionID]Kind{}
	for _, a := range StaticActions() {
		got[a.ID] = a.Kind
	}
	for id, k := range want {
		if got[id] != k {
			t.Errorf("%s kind = %v, want %v", id, got[id], k)
		}
	}
}

func TestStaticActionIDsAreUnique(t *testing.T) {
	seen := map[ActionID]bool{}
	for _, a := range StaticActions() {
		if seen[a.ID] {
			t.Errorf("duplicate action ID %q", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestPerRadioActions(t *testing.T) {
	acts := PerRadioActions([]RadioRef{{ID: 1, Name: "GUARD"}, {ID: 2, Name: "FLEET"}})
	if len(acts) != 4 {
		t.Fatalf("got %d actions, want 4 (2 radios x ptt+select)", len(acts))
	}
	byID := map[ActionID]Action{}
	for _, a := range acts {
		byID[a.ID] = a
	}
	ptt, ok := byID["radio.1.ptt"]
	if !ok {
		t.Fatal("missing radio.1.ptt")
	}
	if ptt.Kind != KindHold {
		t.Error("radio ptt must be KindHold")
	}
	if ptt.Category != CatPerRadio {
		t.Error("radio ptt must be CatPerRadio")
	}
	if sel := byID["radio.2.select"]; sel.Kind != KindPress {
		t.Error("radio select must be KindPress")
	}
}

func TestDefaultsParse(t *testing.T) {
	for id, c := range Defaults() {
		if c.IsZero() {
			t.Errorf("default for %s is zero", id)
		}
	}
}
