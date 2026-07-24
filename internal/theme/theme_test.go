package theme

import "testing"

func TestPresetsAreComplete(t *testing.T) {
	for _, name := range Names() {
		p, ok := Named(name)
		if !ok {
			t.Fatalf("Names() lists %q but Named cannot find it", name)
		}
		colors := map[string]string{
			"Blue": string(p.Blue), "Lavender": string(p.Lavender), "Pink": string(p.Pink),
			"Muted": string(p.Muted), "Red": string(p.Red), "Yellow": string(p.Yellow),
			"Green": string(p.Green), "Bright": string(p.Bright), "Dim": string(p.Dim),
		}
		for role, value := range colors {
			if len(value) != 7 || value[0] != '#' {
				t.Errorf("theme %q role %s has malformed color %q", name, role, value)
			}
		}
	}
}

func TestDefaultExists(t *testing.T) {
	if _, ok := Named(DefaultName); !ok {
		t.Fatalf("default theme %q is not a preset", DefaultName)
	}
	if Default() != presets[DefaultName] {
		t.Fatal("Default() does not match the DefaultName preset")
	}
}

func TestUnknownName(t *testing.T) {
	if _, ok := Named("does-not-exist"); ok {
		t.Fatal("Named accepted an unknown theme")
	}
}
