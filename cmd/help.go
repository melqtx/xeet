package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/melqtx/xeet/internal/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// commandGroups orders the commands the way you meet them: connect first, then
// what you reach for every day, then the rest. A command listed in no group
// still appears, under the last one, so adding a command can never hide it.
var commandGroups = []struct {
	title string
	names []string
}{
	{"start here", []string{"auth", "whoami", "doctor", "logout"}},
	{"every day", []string{"timeline", "post", "theme"}},
	{"extras", []string{"version", "inspect-har", "completion", "help"}},
}

func init() {
	rootCmd.SetHelpFunc(renderHelp)
	rootCmd.SetUsageFunc(func(c *cobra.Command) error {
		renderUsage(c, c.OutOrStderr())
		return nil
	})
	// A command that fails at runtime has already said why; repeating its
	// usage on top of that buries the message. Mistyped flags still get a
	// pointer, from the flag error itself.
	rootCmd.SilenceUsage = true
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\nrun '%s --help' to see what it takes", err, c.CommandPath())
	})
}

// tidyGeneratedCommands builds cobra's two generated commands early so their
// summaries can be rewritten in xeet's voice. Cobra would otherwise add them
// mid-Execute, after every chance to edit them has passed.
func tidyGeneratedCommands() {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	summaries := map[string]string{
		"help":       "show help for any command",
		"completion": "generate a shell completion script",
	}
	for _, sub := range rootCmd.Commands() {
		if short, ok := summaries[sub.Name()]; ok {
			sub.Short = short
		}
	}
}

func renderHelp(c *cobra.Command, args []string) {
	// Cobra adds -h during Execute. Adding it here too means the flag is
	// listed whoever asked for this help, and it is a no-op once it exists.
	c.InitDefaultHelpFlag()
	out := c.OutOrStdout()
	styles := configuredStyles()
	if c.Root() == c {
		renderRootHelp(c, out, styles)
		return
	}
	renderCommandHelp(c, out, styles)
}

func renderRootHelp(c *cobra.Command, out io.Writer, s ui.Styles) {
	paragraph(out, s.Title, ui.Logo)
	fmt.Fprintln(out, s.Dim.Render("post to x from your terminal  ✦"))
	fmt.Fprintln(out)

	width := commandColumn(c)
	usageWidth := max(width, len("xeet <command>"))

	section(out, s, "usage")
	line(out, s, pad("xeet", usageWidth), "browse your timeline")
	line(out, s, pad("xeet <command>", usageWidth), "everything below")
	fmt.Fprintln(out)

	for _, group := range groupCommands(c) {
		section(out, s, group.title)
		for _, sub := range group.commands {
			line(out, s, pad(sub.Name(), width), sub.Short)
		}
		fmt.Fprintln(out)
	}

	printFlags(out, s, c.LocalFlags())
	printExample(out, s, c)
	fmt.Fprintln(out, s.Dim.Render("xeet <command> --help  ·  the whole story on any of them"))
}

func renderCommandHelp(c *cobra.Command, out io.Writer, s ui.Styles) {
	fmt.Fprintln(out, s.Title.Render(c.CommandPath()))
	if c.Short != "" {
		fmt.Fprintln(out, s.Dim.Render(c.Short))
	}
	fmt.Fprintln(out)
	if c.Long != "" {
		paragraph(out, s.Body, c.Long)
		fmt.Fprintln(out)
	}

	section(out, s, "usage")
	fmt.Fprintln(out, "  "+s.Body.Render(c.UseLine()))
	fmt.Fprintln(out)

	if c.HasAvailableSubCommands() {
		section(out, s, "commands")
		width := commandColumn(c)
		for _, sub := range c.Commands() {
			if !sub.IsAvailableCommand() {
				continue
			}
			line(out, s, pad(sub.Name(), width), sub.Short)
		}
		fmt.Fprintln(out)
	}

	printFlags(out, s, c.LocalFlags())
	printExample(out, s, c)
}

// renderUsage is the one-glance version cobra falls back to when a command is
// invoked wrongly.
func renderUsage(c *cobra.Command, out io.Writer) {
	s := configuredStyles()
	fmt.Fprintln(out, s.Dim.Render("usage: ")+s.Body.Render(c.UseLine()))
	fmt.Fprintln(out, s.Dim.Render("run '"+c.CommandPath()+" --help' to see what it takes"))
}

type commandGroup struct {
	title    string
	commands []*cobra.Command
}

// groupCommands buckets the available commands, dropping empty groups and
// sweeping anything unlisted into the last one.
func groupCommands(c *cobra.Command) []commandGroup {
	placed := map[string]bool{}
	available := map[string]*cobra.Command{}
	for _, sub := range c.Commands() {
		if sub.IsAvailableCommand() || sub.Name() == "help" {
			available[sub.Name()] = sub
		}
	}

	groups := make([]commandGroup, 0, len(commandGroups))
	for _, spec := range commandGroups {
		group := commandGroup{title: spec.title}
		for _, name := range spec.names {
			if sub, ok := available[name]; ok {
				group.commands = append(group.commands, sub)
				placed[name] = true
			}
		}
		groups = append(groups, group)
	}

	var leftovers []*cobra.Command
	for _, sub := range c.Commands() {
		if _, ok := available[sub.Name()]; ok && !placed[sub.Name()] {
			leftovers = append(leftovers, sub)
		}
	}
	if len(leftovers) > 0 {
		last := len(groups) - 1
		groups[last].commands = append(groups[last].commands, leftovers...)
	}

	trimmed := groups[:0]
	for _, group := range groups {
		if len(group.commands) > 0 {
			trimmed = append(trimmed, group)
		}
	}
	return trimmed
}

func printFlags(out io.Writer, s ui.Styles, flags *pflag.FlagSet) {
	type row struct{ name, usage string }
	var rows []row
	width := 0
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		name := "    --" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + ", --" + f.Name
		}
		varname, usage := pflag.UnquoteUsage(f)
		if varname != "" {
			name += " " + varname
		}
		// Cobra generates "help for <command>", which reads oddly in a list
		// that is already under that command's own heading.
		if f.Name == "help" && strings.HasPrefix(usage, "help for ") {
			usage = "show this help"
		}
		if len(name) > width {
			width = len(name)
		}
		rows = append(rows, row{name, usage})
	})
	if len(rows) == 0 {
		return
	}
	section(out, s, "flags")
	for _, r := range rows {
		line(out, s, pad(r.name, width+2), r.usage)
	}
	fmt.Fprintln(out)
}

func printExample(out io.Writer, s ui.Styles, c *cobra.Command) {
	if c.Example == "" {
		return
	}
	section(out, s, "examples")
	for _, raw := range strings.Split(strings.TrimRight(c.Example, "\n"), "\n") {
		command, comment, found := strings.Cut(raw, "#")
		if !found {
			fmt.Fprintln(out, s.Body.Render(raw))
			continue
		}
		// Keep the comment in the column the example was written in.
		gap := strings.Repeat(" ", len(command)-len(strings.TrimRight(command, " ")))
		fmt.Fprintln(out, s.Body.Render(strings.TrimRight(command, " "))+
			s.Dim.Render(gap+"# "+strings.TrimSpace(comment)))
	}
	fmt.Fprintln(out)
}

// paragraph prints prose one line at a time. Handing lipgloss a whole block
// would pad every line out to the longest one, leaving trailing whitespace all
// down the right-hand side.
func paragraph(out io.Writer, style lipgloss.Style, text string) {
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if l == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintln(out, style.Render(l))
	}
}

func section(out io.Writer, s ui.Styles, title string) {
	fmt.Fprintln(out, s.Accent.Bold(true).Render(title))
}

func line(out io.Writer, s ui.Styles, name, description string) {
	if description == "" {
		fmt.Fprintln(out, "  "+s.Body.Render(name))
		return
	}
	fmt.Fprintln(out, "  "+s.Body.Render(name)+"  "+s.Dim.Render(description))
}

// commandColumn is the width the descriptions line up at.
func commandColumn(c *cobra.Command) int {
	width := 12
	for _, sub := range c.Commands() {
		if sub.IsAvailableCommand() && len(sub.Name()) > width {
			width = len(sub.Name())
		}
	}
	return width
}

func pad(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}
