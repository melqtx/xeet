package api

// SupportedBrowsers returns the browser choices shown by `xeet auth`, in UI
// preference order. Return a copy so callers cannot mutate the shared list.
func SupportedBrowsers() []string {
	return []string{"Chrome", "Helium", "Firefox", "Brave", "Zen"}
}
