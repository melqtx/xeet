// Package theme holds xeet's color palettes. A palette only recolors the
// interface; layout, spacing, and typography never change with the theme.
package theme

import (
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// Palette names every color role the interface uses. Accent roles (Blue,
// Lavender, Pink) decorate; Muted/Dim/Bright grade text prominence; Red,
// Yellow, and Green carry error, warning, and success meaning.
type Palette struct {
	Blue     lipgloss.Color
	Lavender lipgloss.Color
	Pink     lipgloss.Color
	Muted    lipgloss.Color
	Red      lipgloss.Color
	Yellow   lipgloss.Color
	Green    lipgloss.Color
	Bright   lipgloss.Color
	Dim      lipgloss.Color
}

// DefaultName is the palette used when no theme is configured.
const DefaultName = "tokyonight"

var presets = map[string]Palette{
	"tokyonight": {
		Blue:     "#7AA2F7",
		Lavender: "#BB9AF7",
		Pink:     "#F5C2E7",
		Muted:    "#7D8590",
		Red:      "#FF757F",
		Yellow:   "#E0AF68",
		Green:    "#9ECE6A",
		Bright:   "#C0CAF5",
		Dim:      "#A9B1D6",
	},
	"catppuccin": {
		Blue:     "#89B4FA",
		Lavender: "#B4BEFE",
		Pink:     "#F5C2E7",
		Muted:    "#6C7086",
		Red:      "#F38BA8",
		Yellow:   "#F9E2AF",
		Green:    "#A6E3A1",
		Bright:   "#CDD6F4",
		Dim:      "#BAC2DE",
	},
	"gruvbox": {
		Blue:     "#83A598",
		Lavender: "#D3869B",
		Pink:     "#FE8019",
		Muted:    "#928374",
		Red:      "#FB4934",
		Yellow:   "#FABD2F",
		Green:    "#B8BB26",
		Bright:   "#EBDBB2",
		Dim:      "#BDAE93",
	},
	"nord": {
		Blue:     "#88C0D0",
		Lavender: "#B48EAD",
		Pink:     "#D8B9C8",
		Muted:    "#616E88",
		Red:      "#BF616A",
		Yellow:   "#EBCB8B",
		Green:    "#A3BE8C",
		Bright:   "#ECEFF4",
		Dim:      "#D8DEE9",
	},
	"rosepine": {
		Blue:     "#9CCFD8",
		Lavender: "#C4A7E7",
		Pink:     "#EBBCBA",
		Muted:    "#6E6A86",
		Red:      "#EB6F92",
		Yellow:   "#F6C177",
		Green:    "#9CCFD8",
		Bright:   "#E0DEF4",
		Dim:      "#908CAA",
	},
	"mono": {
		Blue:     "#C8C8C8",
		Lavender: "#B8B8B8",
		Pink:     "#DADADA",
		Muted:    "#707070",
		Red:      "#EFEFEF",
		Yellow:   "#C0C0C0",
		Green:    "#B0B0B0",
		Bright:   "#FFFFFF",
		Dim:      "#9E9E9E",
	},
}

// Default returns the palette xeet ships with.
func Default() Palette { return presets[DefaultName] }

// Named looks up a preset palette by name.
func Named(name string) (Palette, bool) {
	p, ok := presets[name]
	return p, ok
}

// Names lists the available palettes in stable order.
func Names() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
