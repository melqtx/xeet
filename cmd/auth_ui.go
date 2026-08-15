package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/melqtx/xeet/internal/ui"
	"github.com/melqtx/xeet/pkg/api"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type authPhase int

const (
	authPhasePick authPhase = iota
	authPhaseImport
	authPhaseVerify
	authPhaseDone
	authPhaseFail
)

// authPicker walks one browser session from disk to a verified, saved config.
// The picker marks which browsers actually hold an x.com session, because
// guessing wrong is the single most common way this goes sideways.
type authPicker struct {
	ctx    context.Context
	cancel context.CancelFunc
	styles ui.Styles

	phase    authPhase
	browsers []string
	detected map[string]bool
	scanning bool
	cursor   int
	spinner  spinner.Model
	chosen   string
	conn     connection
	err      error
	width    int
	quit     bool
}

type browsersScannedMsg []string

type sessionImportedMsg struct {
	result  *api.LoginResult
	browser string
	err     error
}

type sessionSavedMsg struct {
	conn connection
	err  error
}

func newAuthPicker(ctx context.Context, cancel context.CancelFunc, styles ui.Styles, browser string) authPicker {
	s := spinner.New()
	s.Spinner = spinner.Dot
	p := authPicker{
		ctx:      ctx,
		cancel:   cancel,
		styles:   styles,
		browsers: api.SupportedBrowsers(),
		detected: map[string]bool{},
		scanning: true,
		spinner:  s,
		width:    72,
	}
	// --browser skips straight to the work; there is nothing left to pick.
	if browser != "" {
		p.chosen = browser
		p.phase = authPhaseImport
		p.scanning = false
	}
	return p
}

func (p authPicker) Init() tea.Cmd {
	if p.phase == authPhaseImport {
		return tea.Batch(p.spinner.Tick, importSessionCmd(p.chosen))
	}
	return tea.Batch(p.spinner.Tick, scanBrowsersCmd())
}

// scanBrowsersCmd runs the cookie-store scan in the background. On macOS it
// copies and queries each browser's database, which is slow enough to notice,
// so the picker is already on screen while it works.
func scanBrowsersCmd() tea.Cmd {
	return func() tea.Msg { return browsersScannedMsg(api.DetectBrowsers()) }
}

func importSessionCmd(browser string) tea.Cmd {
	return func() tea.Msg {
		result, resolved, err := api.ImportBrowserSession(browser)
		if resolved == "" {
			resolved = browser
		}
		return sessionImportedMsg{result: result, browser: resolved, err: err}
	}
}

func verifyCmd(ctx context.Context, result *api.LoginResult, browser string) tea.Cmd {
	return func() tea.Msg {
		conn, err := verifyAndSave(ctx, result, browser)
		return sessionSavedMsg{conn: conn, err: err}
	}
}

func (p authPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		return p, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		p.spinner, cmd = p.spinner.Update(msg)
		return p, cmd
	case browsersScannedMsg:
		p.scanning = false
		for _, name := range msg {
			p.detected[name] = true
		}
		// Start on a browser that actually has a session so enter is usually
		// the right answer.
		for i, name := range p.browsers {
			if p.detected[name] {
				p.cursor = i
				break
			}
		}
		return p, nil
	case sessionImportedMsg:
		if msg.err != nil {
			p.phase, p.err = authPhaseFail, msg.err
			return p, nil
		}
		p.phase = authPhaseVerify
		return p, verifyCmd(p.ctx, msg.result, msg.browser)
	case sessionSavedMsg:
		if msg.err != nil {
			p.phase, p.err = authPhaseFail, msg.err
			return p, nil
		}
		p.phase, p.conn = authPhaseDone, msg.conn
		return p, tea.Quit
	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	return p, nil
}

func (p authPicker) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch p.phase {
	case authPhasePick:
		switch key {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.browsers)-1 {
				p.cursor++
			}
		case "enter":
			p.chosen = p.browsers[p.cursor]
			p.phase = authPhaseImport
			p.err = nil
			return p, tea.Batch(p.spinner.Tick, importSessionCmd(p.chosen))
		case "esc", "q", "ctrl+c":
			p.quit = true
			return p, tea.Quit
		}
	case authPhaseFail:
		switch key {
		case "enter":
			p.phase, p.err = authPhasePick, nil
		case "esc", "q", "ctrl+c":
			p.quit = true
			return p, tea.Quit
		}
	default:
		// Mid-flight there is nothing to back out of cleanly, so only an
		// explicit interrupt stops it — and that cancels the request too.
		if key == "ctrl+c" {
			p.cancel()
			p.quit = true
			return p, tea.Quit
		}
	}
	return p, nil
}

func (p authPicker) contentWidth() int {
	w := p.width - 2
	if w > 64 {
		w = 64
	}
	if w < 28 {
		w = 28
	}
	return w
}

// View leaves the success panel on screen when it is done, and nothing at all
// when the user backs out: the command itself reports those outcomes.
func (p authPicker) View() string {
	if p.quit {
		return ""
	}
	w := p.contentWidth()
	s := p.styles

	caption := "let's get you connected"
	switch p.phase {
	case authPhaseImport, authPhaseVerify:
		caption = "hold on tight"
	case authPhaseDone:
		caption = "you're in!"
	case authPhaseFail:
		caption = "that didn't work"
	}
	head := s.RenderLogo(w) + "\n" + s.RenderMascot(w, caption) + "\n\n"

	switch p.phase {
	case authPhaseImport:
		return head + p.renderWorking(w, "reading your x.com session from "+p.chosen+"…",
			"your OS may ask to unlock secure storage")
	case authPhaseVerify:
		return head + p.renderWorking(w, "asking X to verify the session…", "")
	case authPhaseDone:
		return head + p.renderDone(w)
	case authPhaseFail:
		return head + p.renderFail(w)
	default:
		return head + p.renderPick(w)
	}
}

func (p authPicker) renderPick(width int) string {
	s := p.styles
	rows := []string{s.Body.Render("where are you signed in to x.com?"), ""}

	longest := 0
	for _, name := range p.browsers {
		if len(name) > longest {
			longest = len(name)
		}
	}
	for i, name := range p.browsers {
		label := fmt.Sprintf("%-*s", longest, name)
		note := ""
		if !p.scanning {
			note = s.Dim.Render("   not detected")
			if p.detected[name] {
				note = s.OK.Render("   session found")
			}
		}
		if i == p.cursor {
			rows = append(rows, s.Accent.Render("  › ")+s.Body.Bold(true).Render(label)+note)
			continue
		}
		rows = append(rows, "    "+s.Dim.Render(label)+note)
	}

	rows = append(rows, "")
	if p.scanning {
		rows = append(rows, s.Dim.Render("  "+p.spinner.View()+" checking which browsers have a session…"))
	} else {
		rows = append(rows, s.Dim.Render("  not detected? sign in there first, then come back"))
	}
	rows = append(rows, "", s.Dim.Render("↑↓ / j k  move   ·   enter  connect   ·   esc  cancel"))
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...)) + "\n"
}

func (p authPicker) renderWorking(width int, stage, note string) string {
	s := p.styles
	rows := []string{s.Accent.Render(p.spinner.View() + " " + stage)}
	if note != "" {
		rows = append(rows, s.Dim.Render(note))
	}
	rows = append(rows, "", s.Dim.Render("ctrl+c  stop"))
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).
		Render(lipgloss.JoinVertical(lipgloss.Center, rows...)) + "\n"
}

func (p authPicker) renderDone(width int) string {
	s := p.styles
	who := "connected"
	if p.conn.handle != "" {
		who = "connected as @" + p.conn.handle
	}
	rows := []string{
		s.OK.Render("✓ ") + s.Body.Bold(true).Render(who),
		s.Dim.Render("  session read from " + p.conn.source()),
		"",
		s.Accent.Render("  xeet") + s.Dim.Render("             your timeline"),
		s.Accent.Render("  xeet --compose") + s.Dim.Render("   write a post"),
		s.Accent.Render("  xeet theme") + s.Dim.Render("       pick a color theme"),
		s.Accent.Render("  xeet doctor") + s.Dim.Render("      check on this session later"),
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...)) + "\n"
}

func (p authPicker) renderFail(width int) string {
	s := p.styles
	message := "something went wrong"
	if p.err != nil {
		message = p.err.Error()
	}
	// These errors run long and already carry their own advice, so wrap them
	// under a hanging indent rather than letting the tail fall to column zero.
	wrapped := strings.Split(s.Body.Width(width-2).Render(message), "\n")
	rows := []string{s.Err.Render("✗ ") + wrapped[0]}
	for _, line := range wrapped[1:] {
		rows = append(rows, "  "+line)
	}
	rows = append(rows, "", s.Dim.Render("enter  try another browser   ·   esc  give up"))
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...)) + "\n"
}

// runAuthInteractive draws the picker and reports what came of it. The picker
// has already shown progress and failures on screen, so this only prints what
// the final frame deliberately leaves out.
func runAuthInteractive(ctx context.Context, browser string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	styles := configuredStyles()
	model := newAuthPicker(ctx, cancel, styles, browser)
	final, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return err
	}
	picker, ok := final.(authPicker)
	if !ok {
		return nil
	}
	if picker.phase == authPhaseFail && picker.err != nil {
		return picker.err
	}
	if picker.quit && picker.phase != authPhaseDone {
		fmt.Println(styles.Dim.Render("nothing connected · run xeet auth when you're ready"))
	}
	return nil
}
