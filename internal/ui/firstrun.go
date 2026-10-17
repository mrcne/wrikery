package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type firstRunStep int

const (
	stepSignIn firstRunStep = iota
	stepScopes
	stepSyncing
)

type pickerItem struct {
	scope    store.Scope
	depth    int
	checked  bool
	disabled bool
}

type firstRunModel struct {
	step       firstRunStep
	reauth     bool   // a 401 sent the user back here, so a good sign in returns to the main screen instead of asking again
	notice     string // shown above the fields, set after a 401
	browser    bool   // the config names an API app and main gave the hook, so the browser sign in is offered
	needSecret bool   // the config has no secret, a field asks for it
	clientID   string
	useToken   bool            // the token field is in view instead of the browser box, tab flips it
	input      textinput.Model // the permanent token
	secret     textinput.Model
	paste      textinput.Model
	signIn     *SignIn // the browser attempt in flight, nil before enter and after its end
	attempt    int     // counts the browser attempts, the late answer of a cancelled one is told apart by it
	spinner    spinner.Model
	verifying  bool
	errText    string
	name       string
	items      []pickerItem
	cursor     int
	scopes     []store.Scope
	keys       KeyMap
}

func newFirstRun(step firstRunStep, notice string, keys KeyMap, browser, needSecret bool, clientID string) firstRunModel {
	in := textinput.New()
	in.Placeholder = "paste the token here"
	in.EchoMode = textinput.EchoPassword
	// A permanent token is a JWT of several hundred characters and a character limit would cut it without a word,
	// so the input takes any length and scrolls it instead of wrapping out of the border.
	in.Width = 60
	secret := textinput.New()
	secret.Placeholder = "paste the client secret here"
	secret.EchoMode = textinput.EchoPassword
	secret.Width = 60
	paste := textinput.New()
	paste.Placeholder = "http://localhost:8731/callback?code=..."
	paste.Width = 60
	sp := spinner.New(spinner.WithSpinner(spinner.Line))
	f := firstRunModel{step: step, notice: notice, browser: browser, needSecret: needSecret, clientID: clientID,
		input: in, secret: secret, paste: paste, spinner: sp, keys: keys}
	f.focus()
	return f
}

// focus puts the cursor into the field the box shows: the paste field while the browser is out, the secret before that, the token in token mode.
func (f *firstRunModel) focus() {
	f.input.Blur()
	f.secret.Blur()
	f.paste.Blur()
	switch {
	case !f.browser || f.useToken:
		f.input.Focus()
	case f.signIn != nil:
		f.paste.Focus()
	case f.needSecret:
		f.secret.Focus()
	}
}

func (f firstRunModel) Update(msg tea.Msg) (firstRunModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tokenVerifiedMsg:
		f.verifying = false
		if msg.err != nil {
			f.errText = msg.err.Error()
			f.focus()
			return f, nil
		}
		f.name, f.errText = msg.name, ""
		if f.reauth {
			return f, intent(firstRunFinishedMsg{})
		}
		f.step = stepScopes
		return f, nil
	case signInStartedMsg:
		if msg.attempt != f.attempt {
			return f, nil
		}
		if msg.err != nil {
			f.verifying, f.errText = false, msg.err.Error()
			return f, nil
		}
		si := msg.signIn
		f.signIn = &si
		f.focus()
		return f, nil
	case pickerLoadedMsg:
		f.items = f.mergePicker(msg)
		// The list can shrink when a space is unfollowed upstream, and a cursor past the end would panic on the next toggle.
		if f.cursor >= len(f.items) {
			f.cursor = max(0, len(f.items)-1)
		}
		return f, nil
	case scopesLoadedMsg:
		f.scopes = msg.scopes
		return f, nil
	case spinner.TickMsg:
		// The sign in and the sync step animate, so the ticks have to keep coming for either one.
		if !f.verifying && f.step != stepSyncing {
			return f, nil
		}
		var cmd tea.Cmd
		f.spinner, cmd = f.spinner.Update(msg)
		return f, cmd
	case tea.KeyMsg:
		return f.handleKey(msg)
	}
	return f, nil
}

func (f firstRunModel) handleKey(msg tea.KeyMsg) (firstRunModel, tea.Cmd) {
	switch f.step {
	case stepSignIn:
		if f.browser && !f.useToken {
			return f.handleBrowserKey(msg)
		}
		// Tab and not a letter, a token may contain any letter.
		if msg.Type == tea.KeyTab && f.browser && !f.verifying {
			f.useToken, f.errText = false, ""
			f.focus()
			return f, nil
		}
		if msg.Type == tea.KeyEnter {
			token := strings.TrimSpace(f.input.Value())
			if token == "" || f.verifying {
				return f, nil
			}
			f.verifying, f.errText = true, ""
			return f, tea.Batch(intent(firstRunSubmitTokenMsg{token: token}), f.spinner.Tick)
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return f, cmd
	case stepScopes:
		switch {
		case key.Matches(msg, f.keys.Down):
			if f.cursor < len(f.items)-1 {
				f.cursor++
			}
		case key.Matches(msg, f.keys.Up):
			if f.cursor > 0 {
				f.cursor--
			}
		case msg.Type == tea.KeySpace:
			if len(f.items) > 0 && !f.items[f.cursor].disabled {
				f.items[f.cursor].checked = !f.items[f.cursor].checked
			}
		case msg.Type == tea.KeyEnter:
			var selected []store.Scope
			for _, it := range f.items {
				if it.checked && !it.disabled {
					selected = append(selected, it.scope)
				}
			}
			f.step = stepSyncing
			return f, intent(firstRunConfirmScopesMsg{scopes: selected})
		}
	case stepSyncing:
		if msg.Type == tea.KeyEnter {
			return f, intent(firstRunFinishedMsg{})
		}
	}
	return f, nil
}

// handleBrowserKey is the browser box: the secret and enter before the attempt, the paste field and esc while the browser is out.
func (f firstRunModel) handleBrowserKey(msg tea.KeyMsg) (firstRunModel, tea.Cmd) {
	if f.signIn != nil {
		switch msg.Type {
		case tea.KeyEsc:
			// The attempt moves on, so the cancelled one's late answer is dropped by the root.
			f.signIn.Cancel()
			f.signIn, f.verifying, f.errText = nil, false, ""
			f.attempt++
			f.paste.SetValue("")
			f.focus()
			return f, nil
		case tea.KeyEnter:
			// A paste that does not pass shows its error and the attempt goes on, the browser may still come back.
			if address := strings.TrimSpace(f.paste.Value()); address != "" {
				if err := f.signIn.Paste(address); err != nil {
					f.errText = err.Error()
					return f, nil
				}
				f.errText = ""
				f.paste.SetValue("")
			}
			return f, nil
		case tea.KeyCtrlY:
			// The address breaks across lines inside the box, so a click on it in the terminal gets one line.
			return f, intent(firstRunCopyAddressMsg{address: f.signIn.URL})
		}
		var cmd tea.Cmd
		f.paste, cmd = f.paste.Update(msg)
		return f, cmd
	}
	switch msg.Type {
	case tea.KeyTab:
		// The hook is starting the attempt, a switch now would leave it running behind the token field.
		if f.verifying {
			return f, nil
		}
		f.useToken, f.errText = true, ""
		f.focus()
		return f, nil
	case tea.KeyEnter:
		if f.verifying {
			return f, nil
		}
		secret := strings.TrimSpace(f.secret.Value())
		if f.needSecret && secret == "" {
			f.errText = "the client secret is needed"
			return f, nil
		}
		f.attempt++
		f.verifying, f.errText = true, ""
		return f, tea.Batch(intent(firstRunStartSignInMsg{attempt: f.attempt, secret: secret}), f.spinner.Tick)
	}
	var cmd tea.Cmd
	f.secret, cmd = f.secret.Update(msg)
	return f, cmd
}

// mergePicker rebuilds the checklist from the store and keeps what the user already ticked.
// The first list starts from the set the cache follows: after a sign out and a new sign in that set is still there.
func (f firstRunModel) mergePicker(msg pickerLoadedMsg) []pickerItem {
	if len(f.items) == 0 {
		return pickerItems(msg, followedSet(msg.followed))
	}
	checked := map[string]bool{}
	for _, it := range f.items {
		checked[it.scope.ID] = it.checked
	}
	return pickerItems(msg, checked)
}

func (f firstRunModel) View(th Theme, width, height int) string {
	accent := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	w := min(width-4, 72)
	if f.step == stepSignIn && f.browser && !f.useToken && f.signIn != nil {
		// The authorize address is long, a wider box breaks it into fewer lines.
		w = min(width-4, 110)
	}
	steps := []string{"1 Sign in", "2 Follow", "3 Sync"}
	var head []string
	for i, s := range steps {
		if firstRunStep(i) == f.step {
			head = append(head, accent.Render(s))
		} else {
			head = append(head, muted.Render(s))
		}
	}
	var body strings.Builder
	body.WriteString(strings.Join(head, muted.Render("  ->  ")) + "\n\n")
	switch f.step {
	case stepSignIn:
		if f.notice != "" {
			body.WriteString(lipgloss.NewStyle().Foreground(th.Warn).Render(f.notice) + "\n\n")
		}
		f.viewSignIn(&body, th, muted, w)
	case stepScopes:
		body.WriteString("Hello " + f.name + ". Pick the spaces and projects to follow.\n")
		body.WriteString(muted.Render("Only these are synced and searchable.") + "\n")
		body.WriteString(muted.Render("Your own tasks are always included.") + "\n\n")
		if len(f.items) == 0 {
			body.WriteString(f.spinner.View() + " loading spaces")
		}
		for i, it := range f.items {
			mark := "[ ]"
			if it.checked {
				mark = "[x]"
			}
			line := strings.Repeat("  ", it.depth) + mark + " " + it.scope.Title
			if it.disabled {
				line = muted.Render(line)
			}
			if i == f.cursor {
				line = accent.Render(th.Glyphs.Cursor+" ") + line
			} else {
				line = "  " + line
			}
			body.WriteString(line + "\n")
		}
		body.WriteString("\n" + muted.Render("space to toggle, enter to confirm"))
	case stepSyncing:
		body.WriteString("Syncing the followed scopes.\n\n")
		for _, sc := range f.scopes {
			glyph := f.spinner.View()
			if sc.Cursor != "" {
				glyph = lipgloss.NewStyle().Foreground(th.Success).Render(th.Glyphs.Completed)
			}
			body.WriteString(glyph + " " + sc.Title + "\n")
		}
		body.WriteString("\n" + muted.Render("enter to open the app, tasks keep arriving in the background"))
	}
	box := th.box("Welcome to wrikery", body.String(), w, min(height, lipgloss.Height(body.String())+2), true)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// viewSignIn draws step 1: the browser box waiting or idle, or the token field.
func (f firstRunModel) viewSignIn(body *strings.Builder, th Theme, muted lipgloss.Style, w int) {
	switch {
	case f.browser && !f.useToken && f.signIn != nil:
		body.WriteString(f.spinner.View() + " waiting for the browser\n\n")
		body.WriteString(muted.Render("If it did not open, use this address:") + "\n")
		body.WriteString(muted.Width(w-2).Render(f.signIn.URL) + "\n\n")
		if f.signIn.Listening {
			body.WriteString("Or paste the address the browser lands on:\n")
		} else {
			body.WriteString("Or paste the address the browser lands on, nothing listens on the port of the redirect address:\n")
		}
		body.WriteString(f.paste.View() + "\n\n")
		if f.errText != "" {
			body.WriteString(th.errorLine(f.errText, w-2) + "\n")
		}
		body.WriteString(muted.Render("enter to send the address, ctrl+y to copy it, esc to start over"))
	case f.browser && !f.useToken:
		body.WriteString("Sign in through the browser.\n")
		body.WriteString(muted.Render("wrikery opens Wrike's sign in page for the app "+f.clientID+".") + "\n\n")
		if f.needSecret {
			body.WriteString("Client secret " + muted.Render("from the same API app, kept in the keychain") + "\n")
			body.WriteString(f.secret.View() + "\n\n")
		}
		switch {
		case f.verifying:
			body.WriteString(f.spinner.View() + " starting the sign in")
		case f.errText != "":
			body.WriteString(th.errorLine(f.errText, w-2))
		default:
			body.WriteString(muted.Render("enter to open the browser, tab to paste a permanent token instead"))
		}
	default:
		body.WriteString("Paste a permanent access token.\n")
		body.WriteString(muted.Render("Create one in the Wrike App Console (Get token). Wrike shows it once.") + "\n")
		body.WriteString(muted.Render("https://www.wrike.com/appconsole.htm?#/api") + "\n\n")
		body.WriteString(f.input.View() + "\n\n")
		switch {
		case f.verifying:
			body.WriteString(f.spinner.View() + " checking the token")
		case f.errText != "":
			body.WriteString(th.errorLine(f.errText, w-2))
		case f.browser:
			body.WriteString(muted.Render("enter to continue, tab to sign in through the browser instead"))
		default:
			body.WriteString(muted.Render("enter to continue"))
		}
	}
}
