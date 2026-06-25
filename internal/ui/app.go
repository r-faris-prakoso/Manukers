package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/ui/views"
)

// ─── View IDs ────────────────────────────────────────────────────────────────

const (
	viewOverview = "overview"
	viewEC2      = "ec2"
	viewLB       = "lb"
	viewTG       = "tg"
	viewSG       = "sg"
	viewEKS      = "eks"
	viewGraph    = "graph"
	viewECR      = "ecr"
	viewS3       = "s3"

	// cacheTTL: data older than this is considered stale and will be
	// refreshed lazily the next time the view is activated.
	cacheTTL = 30 * time.Second
)

// commandAliases maps typed `:commands` to view IDs.
var commandAliases = map[string]string{
	"overview":       viewOverview,
	"home":           viewOverview,
	"ec2":            viewEC2,
	"instances":      viewEC2,
	"lb":             viewLB,
	"loadbalancer":   viewLB,
	"loadbalancers":  viewLB,
	"alb":            viewLB,
	"nlb":            viewLB,
	"tg":             viewTG,
	"targetgroup":    viewTG,
	"targetgroups":   viewTG,
	"sg":             viewSG,
	"securitygroup":  viewSG,
	"securitygroups": viewSG,
	"eks":            viewEKS,
	"k8s":            viewEKS,
	"kubernetes":     viewEKS,
	"graph":          viewGraph,
	"connection":     viewGraph,
	"ecr":            viewECR,
	"registry":       viewECR,
	"repositories":   viewECR,
	"s3":             viewS3,
	"buckets":        viewS3,
	"storage":        viewS3,
}

// refreshable is implemented by every view.
type refreshable interface {
	Refresh(ctx context.Context)
}

// App is the main TUI application.
type App struct {
	tviewApp    *tview.Application
	layout      *tview.Flex
	header      *tview.TextView
	navBar      *tview.TextView
	mainContent *tview.Pages
	footer      *tview.TextView

	// Command bar
	cmdArea    *tview.Pages
	cmdInput   *tview.InputField
	cmdHint    *tview.TextView
	cmdVisible bool

	client      *aws.Client
	currentView string

	overviewView *views.OverviewView
	ec2View      *views.EC2View
	lbView       *views.LBView
	tgView       *views.TGView
	sgView       *views.SGView
	eksView      *views.EKSView
	graphView    *views.GraphView
	ecrView      *views.ECRView
	s3View       *views.S3View

	// Per-view lazy-load cache.  Protected by mu.
	viewFetched  map[string]time.Time // last successful fetch time
	viewFetching map[string]bool      // fetch in progress?

	lastRefresh time.Time
	mu          sync.Mutex
}

// NewApp creates and initializes the TUI application.
func NewApp(client *aws.Client) *App {
	a := &App{
		tviewApp:     tview.NewApplication(),
		mainContent:  tview.NewPages(),
		cmdArea:      tview.NewPages(),
		client:       client,
		currentView:  viewEC2,
		viewFetched:  make(map[string]time.Time),
		viewFetching: make(map[string]bool),
	}

	a.buildViews()
	a.buildLayout()
	a.setupInput()
	a.updateHeader()
	a.updateNavBar()

	// Lazy-load only the starting view; everything else loads on demand.
	go a.loadView(viewEC2, false)

	// Auto-refresh: revisit the current view every 30 s.
	// loadView respects cacheTTL so it is a no-op if data is fresh.
	go func() {
		ticker := time.NewTicker(cacheTTL)
		defer ticker.Stop()
		for range ticker.C {
			go a.loadView(a.currentView, false)
		}
	}()

	return a
}

// Run starts the event loop.
func (a *App) Run() error {
	return a.tviewApp.Run()
}

// ─── Lazy loading ────────────────────────────────────────────────────────────

// loadView loads a view's data if it is stale or not yet fetched.
// Pass force=true to bypass the cache (e.g. when the user presses r).
func (a *App) loadView(id string, force bool) {
	v := a.viewByID(id)
	if v == nil {
		return
	}

	a.mu.Lock()
	fetching := a.viewFetching[id]
	lastFetch := a.viewFetched[id]
	a.mu.Unlock()

	if fetching {
		return // already in flight
	}
	if !force && !lastFetch.IsZero() && time.Since(lastFetch) < cacheTTL {
		return // data is fresh
	}

	a.mu.Lock()
	a.viewFetching[id] = true
	a.mu.Unlock()

	ctx := context.Background()
	v.Refresh(ctx)

	a.mu.Lock()
	a.viewFetched[id] = time.Now()
	a.viewFetching[id] = false
	a.lastRefresh = time.Now()
	a.mu.Unlock()

	a.tviewApp.QueueUpdateDraw(func() {
		a.updateHeader()
	})
}

// viewByID returns the refreshable interface for a given view ID.
func (a *App) viewByID(id string) refreshable {
	switch id {
	case viewOverview:
		return a.overviewView
	case viewEC2:
		return a.ec2View
	case viewLB:
		return a.lbView
	case viewTG:
		return a.tgView
	case viewSG:
		return a.sgView
	case viewEKS:
		return a.eksView
	case viewGraph:
		return a.graphView
	case viewECR:
		return a.ecrView
	case viewS3:
		return a.s3View
	}
	return nil
}

// ─── Layout ──────────────────────────────────────────────────────────────────

func (a *App) buildViews() {
	a.overviewView = views.NewOverviewView(a.tviewApp, a.client)
	a.ec2View = views.NewEC2View(a.tviewApp, a.client)
	a.lbView = views.NewLBView(a.tviewApp, a.client)
	a.tgView = views.NewTGView(a.tviewApp, a.client)
	a.sgView = views.NewSGView(a.tviewApp, a.client)
	a.eksView = views.NewEKSView(a.tviewApp, a.client)
	a.graphView = views.NewGraphView(a.tviewApp, a.client)
	a.ecrView = views.NewECRView(a.tviewApp, a.client)
	a.s3View = views.NewS3View(a.tviewApp, a.client)

	a.mainContent.AddPage(viewOverview, a.overviewView.GetContent(), true, false)
	a.mainContent.AddPage(viewEC2, a.ec2View.GetContent(), true, true)
	a.mainContent.AddPage(viewLB, a.lbView.GetContent(), true, false)
	a.mainContent.AddPage(viewTG, a.tgView.GetContent(), true, false)
	a.mainContent.AddPage(viewSG, a.sgView.GetContent(), true, false)
	a.mainContent.AddPage(viewEKS, a.eksView.GetContent(), true, false)
	a.mainContent.AddPage(viewGraph, a.graphView.GetContent(), true, false)
	a.mainContent.AddPage(viewECR, a.ecrView.GetContent(), true, false)
	a.mainContent.AddPage(viewS3, a.s3View.GetContent(), true, false)
}

func (a *App) buildLayout() {
	a.header = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	a.header.SetBackgroundColor(tcell.ColorDarkSlateBlue)

	a.navBar = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	a.navBar.SetBackgroundColor(tcell.ColorDarkSlateGray)

	a.footer = tview.NewTextView().SetDynamicColors(true)
	a.footer.SetBackgroundColor(tcell.ColorDarkSlateGray)
	a.footer.SetText(
		" [yellow]:[white]cmd  [yellow]1-9[white] switch  [yellow]Enter[white] select  " +
			"[yellow]Esc[white] back  [yellow]/[white] filter  [yellow]r[white] refresh  [yellow]q[white] quit",
	)

	a.cmdHint = tview.NewTextView().SetDynamicColors(true)
	a.cmdHint.SetBackgroundColor(tcell.ColorDarkSlateGray)

	a.cmdInput = tview.NewInputField().
		SetLabel("[yellow:darkslategray:b]:[white:darkslategray:-]").
		SetFieldBackgroundColor(tcell.ColorDarkSlateGray).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorYellow)

	a.cmdInput.SetChangedFunc(func(text string) {
		a.updateCmdHint(text)
	})
	a.cmdInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			cmd := strings.TrimSpace(a.cmdInput.GetText())
			a.cmdInput.SetText("")
			a.hideCommandBar()
			if cmd != "" {
				a.executeCommand(cmd)
			}
		case tcell.KeyEscape, tcell.KeyTab:
			a.cmdInput.SetText("")
			a.hideCommandBar()
		}
	})

	cmdLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.cmdHint, 1, 0, false).
		AddItem(a.cmdInput, 1, 0, true)

	a.cmdArea.AddPage("footer", a.footer, true, true)
	a.cmdArea.AddPage("command", cmdLayout, true, false)

	a.layout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(a.navBar, 1, 0, false).
		AddItem(a.mainContent, 0, 1, true).
		AddItem(a.cmdArea, 2, 0, false)

	a.tviewApp.SetRoot(a.layout, true)
}

func (a *App) updateHeader() {
	a.mu.Lock()
	lr := a.lastRefresh
	a.mu.Unlock()

	sync := "never"
	if !lr.IsZero() {
		sync = lr.Format("15:04:05")
	}
	a.header.SetText(fmt.Sprintf(
		" [::b][aqua]MANUKERS[-:-:-]  [darkgray]region:[white] %s  [darkgray]last sync:[white] %s  [darkgray]cache: %s[-]",
		a.client.Region, sync, cacheTTL,
	))
}

func (a *App) updateNavBar() {
	type entry struct{ shortcut, name, view string }
	tabs := []entry{
		{"1", "Overview", viewOverview},
		{"2", "EC2", viewEC2},
		{"3", "Load Balancers", viewLB},
		{"4", "Target Groups", viewTG},
		{"5", "Security Groups", viewSG},
		{"6", "EKS", viewEKS},
		{"7", "ECR", viewECR},
		{"8", "S3", viewS3},
		{"9", "Graph", viewGraph},
	}
	text := " "
	for _, t := range tabs {
		if t.view == a.currentView {
			text += fmt.Sprintf("[black:aqua:b] %s: %s [-:-:-]  ", t.shortcut, t.name)
		} else {
			// Show a dot if data has been fetched for this view
			a.mu.Lock()
			fetched := !a.viewFetched[t.view].IsZero()
			a.mu.Unlock()
			dot := ""
			if fetched {
				dot = "[darkgray]·[-]"
			}
			text += fmt.Sprintf("[darkgray]<%s>[-] %s%s  ", t.shortcut, t.name, dot)
		}
	}
	a.navBar.SetText(text)
}

// ─── Command Bar ─────────────────────────────────────────────────────────────

func (a *App) showCommandBar() {
	a.cmdVisible = true
	a.updateCmdHint("")
	a.cmdArea.SwitchToPage("command")
	a.tviewApp.SetFocus(a.cmdInput)
}

func (a *App) hideCommandBar() {
	a.cmdVisible = false
	a.cmdArea.SwitchToPage("footer")
	a.setFocusForView(a.currentView)
}

func (a *App) updateCmdHint(typed string) {
	all := allCommandKeys()
	typed = strings.ToLower(typed)

	var matches, rest []string
	for _, k := range all {
		if typed == "" || strings.HasPrefix(k, typed) {
			matches = append(matches, k)
		} else {
			rest = append(rest, k)
		}
	}

	hint := " "
	for _, m := range matches {
		hint += fmt.Sprintf("[aqua]%s[-]  ", m)
	}
	for _, r := range rest {
		hint += fmt.Sprintf("[darkgray]%s[-]  ", r)
	}
	a.cmdHint.SetText(hint)
}

func allCommandKeys() []string {
	seen := map[string]bool{}
	var keys []string
	for k := range commandAliases {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func (a *App) executeCommand(cmd string) {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	if cmd == "q" || cmd == "quit" || cmd == "exit" {
		a.tviewApp.Stop()
		return
	}
	if view, ok := commandAliases[cmd]; ok {
		a.switchTo(view)
		return
	}
	// Prefix match — pick shortest
	var best string
	for alias, view := range commandAliases {
		if strings.HasPrefix(alias, cmd) {
			if best == "" || len(alias) < len(best) {
				best = view
			}
		}
	}
	if best != "" {
		a.switchTo(best)
	}
}

// ─── Input Handling ───────────────────────────────────────────────────────────

func (a *App) setupInput() {
	a.tviewApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if a.cmdVisible {
			return event
		}
		switch event.Rune() {
		case ':':
			a.showCommandBar()
			return nil
		case 'q', 'Q':
			a.tviewApp.Stop()
			return nil
		case 'r', 'R':
			// Force-refresh current view (bypass cache)
			go a.loadView(a.currentView, true)
			return nil
		case '1':
			a.switchTo(viewOverview)
			return nil
		case '2':
			a.switchTo(viewEC2)
			return nil
		case '3':
			a.switchTo(viewLB)
			return nil
		case '4':
			a.switchTo(viewTG)
			return nil
		case '5':
			a.switchTo(viewSG)
			return nil
		case '6':
			a.switchTo(viewEKS)
			return nil
		case '7':
			a.switchTo(viewECR)
			return nil
		case '8':
			a.switchTo(viewS3)
			return nil
		case '9':
			a.switchTo(viewGraph)
			return nil
		}
		return event
	})
}

// switchTo changes the active page instantly, then triggers a lazy data load
// in the background.  Navigation is always immediate regardless of network.
func (a *App) switchTo(view string) {
	a.currentView = view
	// Page switch is synchronous — happens before any network I/O.
	a.mainContent.SwitchToPage(view)
	a.tviewApp.QueueUpdateDraw(func() {
		a.updateNavBar()
		a.setFocusForView(view)
	})
	// Load data in background; no-op if cache is still fresh.
	go a.loadView(view, false)
}

func (a *App) setFocusForView(view string) {
	switch view {
	case viewOverview:
		a.tviewApp.SetFocus(a.overviewView.GetFocusable())
	case viewEC2:
		a.tviewApp.SetFocus(a.ec2View.GetFocusable())
	case viewLB:
		a.tviewApp.SetFocus(a.lbView.GetFocusable())
	case viewTG:
		a.tviewApp.SetFocus(a.tgView.GetFocusable())
	case viewSG:
		a.tviewApp.SetFocus(a.sgView.GetFocusable())
	case viewEKS:
		a.tviewApp.SetFocus(a.eksView.GetFocusable())
	case viewGraph:
		a.tviewApp.SetFocus(a.graphView.GetFocusable())
	case viewECR:
		a.tviewApp.SetFocus(a.ecrView.GetFocusable())
	case viewS3:
		a.tviewApp.SetFocus(a.s3View.GetFocusable())
	}
}
