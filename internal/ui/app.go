package ui

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/ui/views"
)

const footerNormal = " [yellow]Esc[-] Back  [yellow]r[-] Refresh  [yellow]/[-] Filter  [yellow]:[- ]Command  [yellow]q[-] Quit"

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ─── View IDs ────────────────────────────────────────────────────────────────

const (
	viewEC2   = "ec2"
	viewLB    = "lb"
	viewTG    = "tg"
	viewSG    = "sg"
	viewEKS   = "eks"
	viewGraph = "graph"
	viewECR   = "ecr"
	viewS3    = "s3"

	pagePicker   = "picker"
	pageResource = "resource" // wraps all resource views
)

// commandAliases maps `:cmd` strings to view IDs.
var commandAliases = map[string]string{
	"ec2": viewEC2, "instances": viewEC2,
	"lb": viewLB, "loadbalancer": viewLB, "loadbalancers": viewLB, "alb": viewLB, "nlb": viewLB,
	"tg": viewTG, "targetgroup": viewTG, "targetgroups": viewTG,
	"sg": viewSG, "securitygroup": viewSG, "securitygroups": viewSG,
	"eks": viewEKS, "k8s": viewEKS, "kubernetes": viewEKS,
	"graph": viewGraph, "connection": viewGraph,
	"ecr": viewECR, "registry": viewECR, "repositories": viewECR,
	"s3": viewS3, "buckets": viewS3, "storage": viewS3,
}

// App is the main TUI application.
type App struct {
	tviewApp *tview.Application

	// Root page stack: picker OR resource view.
	pages        *tview.Pages
	resourcePages *tview.Pages // holds the individual resource views

	// Command bar (shown over the footer area).
	cmdInput   *tview.InputField
	cmdHint    *tview.TextView
	cmdArea    *tview.Pages
	cmdVisible bool

	client      *aws.Client
	currentView string // which resource is active inside resourcePages

	pickerView   *views.PickerView
	overviewView *views.OverviewView // kept for completeness; not in picker
	ec2View      *views.EC2View
	lbView       *views.LBView
	tgView       *views.TGView
	sgView       *views.SGView
	eksView      *views.EKSView
	graphView    *views.GraphView
	ecrView      *views.ECRView
	s3View       *views.S3View

	// Per-view fetch tracking (load once, manual refresh via r).
	// No auto-refresh — API calls only happen on explicit user action.
	viewFetched  map[string]bool
	viewFetching map[string]bool
	mu           sync.Mutex

	// Footer spinner
	footer       *tview.TextView
	spinCount    atomic.Int32 // number of concurrent loads in flight
	spinStop     chan struct{} // closed when the last load finishes
	spinMu       sync.Mutex
}

// NewApp creates and initialises the application.  No AWS calls are made here.
func NewApp(client *aws.Client) *App {
	a := &App{
		tviewApp:     tview.NewApplication(),
		pages:        tview.NewPages(),
		resourcePages: tview.NewPages(),
		cmdArea:      tview.NewPages(),
		client:       client,
		viewFetched:  make(map[string]bool),
		viewFetching: make(map[string]bool),
	}

	a.buildResourceViews()
	a.buildLayout()
	a.setupGlobalInput()

	return a
}

func (a *App) Run() error {
	return a.tviewApp.Run()
}

// ─── Layout ──────────────────────────────────────────────────────────────────

func (a *App) buildResourceViews() {
	a.overviewView = views.NewOverviewView(a.tviewApp, a.client)
	a.ec2View = views.NewEC2View(a.tviewApp, a.client)
	a.lbView = views.NewLBView(a.tviewApp, a.client)
	a.tgView = views.NewTGView(a.tviewApp, a.client)
	a.sgView = views.NewSGView(a.tviewApp, a.client)
	a.eksView = views.NewEKSView(a.tviewApp, a.client)
	a.graphView = views.NewGraphView(a.tviewApp, a.client)
	a.ecrView = views.NewECRView(a.tviewApp, a.client)
	a.s3View = views.NewS3View(a.tviewApp, a.client)

	a.resourcePages.AddPage(viewEC2, a.ec2View.GetContent(), true, false)
	a.resourcePages.AddPage(viewLB, a.lbView.GetContent(), true, false)
	a.resourcePages.AddPage(viewTG, a.tgView.GetContent(), true, false)
	a.resourcePages.AddPage(viewSG, a.sgView.GetContent(), true, false)
	a.resourcePages.AddPage(viewEKS, a.eksView.GetContent(), true, false)
	a.resourcePages.AddPage(viewGraph, a.graphView.GetContent(), true, false)
	a.resourcePages.AddPage(viewECR, a.ecrView.GetContent(), true, false)
	a.resourcePages.AddPage(viewS3, a.s3View.GetContent(), true, false)
}

func (a *App) buildLayout() {
	// Picker — shown first, no AWS calls.
	a.pickerView = views.NewPickerView(a.tviewApp, a.client.Region, func(viewID string) {
		a.openResource(viewID)
	})

	// Footer for the resource view area.
	a.footer = tview.NewTextView().SetDynamicColors(true)
	a.footer.SetBackgroundColor(tcell.ColorDarkSlateGray)
	a.footer.SetText(footerNormal)
	footer := a.footer

	// Command bar: hint + input (shown instead of footer when : is pressed).
	a.cmdHint = tview.NewTextView().SetDynamicColors(true)
	a.cmdHint.SetBackgroundColor(tcell.ColorDarkSlateGray)

	a.cmdInput = tview.NewInputField().
		SetLabel("[yellow:darkslategray:b]:[white:darkslategray:-]").
		SetFieldBackgroundColor(tcell.ColorDarkSlateGray).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorYellow)
	a.cmdInput.SetChangedFunc(func(text string) { a.updateCmdHint(text) })
	a.cmdInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			cmd := strings.TrimSpace(a.cmdInput.GetText())
			a.cmdInput.SetText("")
			a.hideCmd()
			if cmd != "" {
				a.executeCommand(cmd)
			}
		default: // Esc, Tab
			a.cmdInput.SetText("")
			a.hideCmd()
		}
	})

	cmdLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.cmdHint, 1, 0, false).
		AddItem(a.cmdInput, 1, 0, true)

	a.cmdArea.AddPage("footer", footer, true, true)
	a.cmdArea.AddPage("command", cmdLayout, true, false)

	// Resource view wrapper: content + bottom bar.
	resourceLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.resourcePages, 0, 1, true).
		AddItem(a.cmdArea, 2, 0, false)

	// Root pages: picker or resource layout.
	a.pages.AddPage(pagePicker, a.pickerView.GetContent(), true, true)
	a.pages.AddPage(pageResource, resourceLayout, true, false)

	a.tviewApp.SetRoot(a.pages, true).SetFocus(a.pickerView.GetFocusable())
}

// ─── Navigation ──────────────────────────────────────────────────────────────

// openResource switches to the resource view and lazily loads its data.
// Navigation is always instant; the load happens in a background goroutine.
func (a *App) openResource(viewID string) {
	a.currentView = viewID
	a.resourcePages.SwitchToPage(viewID)
	a.pages.SwitchToPage(pageResource)
	a.setFocus(viewID)
	go a.loadOnce(viewID)
}

// goToPicker returns to the picker screen.  Cached data is retained.
func (a *App) goToPicker() {
	a.hideCmd()
	a.pages.SwitchToPage(pagePicker)
	a.tviewApp.SetFocus(a.pickerView.GetFocusable())
}

// loadOnce fetches data for a view only if it hasn't been loaded yet this
// session.  Pressing r calls loadForce, which always re-fetches.
func (a *App) loadOnce(id string) {
	a.mu.Lock()
	already := a.viewFetched[id]
	fetching := a.viewFetching[id]
	a.mu.Unlock()

	if already || fetching {
		return
	}
	a.doLoad(id)
}

func (a *App) loadForce(id string) {
	a.mu.Lock()
	if a.viewFetching[id] {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.doLoad(id)
}

func (a *App) doLoad(id string) {
	v := a.viewByID(id)
	if v == nil {
		return
	}
	a.mu.Lock()
	a.viewFetching[id] = true
	a.mu.Unlock()

	a.startSpinner(id)
	v.Refresh(context.Background())
	a.stopSpinner()

	a.mu.Lock()
	a.viewFetched[id] = true
	a.viewFetching[id] = false
	a.mu.Unlock()
}

func (a *App) startSpinner(label string) {
	a.spinMu.Lock()
	defer a.spinMu.Unlock()
	if a.spinCount.Add(1) == 1 {
		// First load in flight — spawn the animation goroutine.
		stop := make(chan struct{})
		a.spinStop = stop
		go func() {
			tick := time.NewTicker(80 * time.Millisecond)
			defer tick.Stop()
			frame := 0
			for {
				select {
				case <-stop:
					a.tviewApp.QueueUpdateDraw(func() {
						a.footer.SetText(footerNormal)
					})
					return
				case <-tick.C:
					f := spinFrames[frame%len(spinFrames)]
					frame++
					lbl := label
					a.tviewApp.QueueUpdateDraw(func() {
						a.footer.SetText(" [aqua]" + f + "[-] Loading " + lbl + "…  [darkgray]" +
							footerNormal[1:] + "[-]")
					})
				}
			}
		}()
	}
}

func (a *App) stopSpinner() {
	a.spinMu.Lock()
	defer a.spinMu.Unlock()
	if a.spinCount.Add(-1) == 0 && a.spinStop != nil {
		close(a.spinStop)
		a.spinStop = nil
	}
}

type refreshable interface{ Refresh(ctx context.Context) }

func (a *App) viewByID(id string) refreshable {
	switch id {
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

func (a *App) setFocus(id string) {
	switch id {
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

// isRootFocusable returns true when the focused primitive is the top-level
// list of a resource view (not a drill-down detail).  Used to decide whether
// Esc should return to the picker.
func (a *App) isRootFocusable(prim tview.Primitive) bool {
	return prim == a.ec2View.GetFocusable() ||
		prim == a.lbView.GetFocusable() ||
		prim == a.tgView.GetFocusable() ||
		prim == a.sgView.GetFocusable() ||
		prim == a.eksView.GetFocusable() ||
		prim == a.graphView.GetFocusable() ||
		prim == a.ecrView.GetFocusable() ||
		prim == a.s3View.GetFocusable()
}

// ─── Command Bar ─────────────────────────────────────────────────────────────

func (a *App) showCmd() {
	a.cmdVisible = true
	a.updateCmdHint("")
	a.cmdArea.SwitchToPage("command")
	a.tviewApp.SetFocus(a.cmdInput)
}

func (a *App) hideCmd() {
	a.cmdVisible = false
	a.cmdArea.SwitchToPage("footer")
	if a.currentView != "" {
		a.setFocus(a.currentView)
	} else {
		a.tviewApp.SetFocus(a.pickerView.GetFocusable())
	}
}

func (a *App) updateCmdHint(typed string) {
	keys := allCmdKeys()
	typed = strings.ToLower(typed)
	var matches, rest []string
	for _, k := range keys {
		if typed == "" || strings.HasPrefix(k, typed) {
			matches = append(matches, k)
		} else {
			rest = append(rest, k)
		}
	}
	hint := " "
	for _, m := range matches {
		hint += "[aqua]" + m + "[-]  "
	}
	for _, r := range rest {
		hint += "[darkgray]" + r + "[-]  "
	}
	a.cmdHint.SetText(hint)
}

func allCmdKeys() []string {
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
	if cmd == "back" || cmd == "picker" || cmd == "home" {
		a.goToPicker()
		return
	}
	// Exact match
	if id, ok := commandAliases[cmd]; ok {
		a.openResource(id)
		return
	}
	// Prefix match — shortest alias wins
	var best string
	for alias, id := range commandAliases {
		if strings.HasPrefix(alias, cmd) {
			if best == "" || len(alias) < len(best) {
				best = id
			}
		}
	}
	if best != "" {
		a.openResource(best)
	}
}

// ─── Global Input ─────────────────────────────────────────────────────────────

func (a *App) setupGlobalInput() {
	a.tviewApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// While the command bar is open, pass everything through.
		if a.cmdVisible {
			return event
		}
		// While any InputField is focused (e.g. the / filter bar in any view),
		// pass everything through so the user can type freely — including 'r',
		// 'q', ':', etc. — without triggering global shortcuts.
		if _, isInput := a.tviewApp.GetFocus().(*tview.InputField); isInput {
			return event
		}

		switch event.Rune() {
		case 'q', 'Q':
			a.tviewApp.Stop()
			return nil

		case ':':
			a.showCmd()
			return nil

		case 'r', 'R':
			// Force-reload the active resource (bypass the "already loaded" flag).
			if a.currentView != "" {
				go a.loadForce(a.currentView)
			}
			return nil
		}

		// Esc: if focus is on the root list of a resource view, go back to
		// the picker.  Otherwise let the view handle its own Esc (e.g. detail
		// → list transitions).
		if event.Key() == tcell.KeyEscape {
			if a.isRootFocusable(a.tviewApp.GetFocus()) {
				a.goToPicker()
				return nil
			}
		}

		return event
	})
}
