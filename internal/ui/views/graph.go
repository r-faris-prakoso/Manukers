package views

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/theme"
)

// GraphView shows the connection graph: LB → Listeners → Rules → Target Groups → Targets.
type GraphView struct {
	app    *tview.Application
	client *aws.Client

	pages    *tview.Pages
	flex     *tview.Flex
	lbList   *tview.List
	treeView *tview.TreeView
	helpText *tview.TextView

	lbs     []aws.LoadBalancer
	tgCache map[string]aws.TargetGroup // ARN → TG
	filter  string

	// debounce prevents buildTree from firing on every arrow-key event.
	debounce   *time.Timer
	debounceMu sync.Mutex
}

func NewGraphView(app *tview.Application, client *aws.Client) *GraphView {
	v := &GraphView{
		app:     app,
		client:  client,
		tgCache: make(map[string]aws.TargetGroup),
		pages:   tview.NewPages(),
	}

	v.lbList = tview.NewList().
		SetHighlightFullLine(true).
		SetWrapAround(true)
	v.lbList.SetBorder(true).SetTitle(" Load Balancers  </> Filter ")
	v.lbList.SetSelectedBackgroundColor(tcell.ColorNavy)

	root := tview.NewTreeNode("Select a Load Balancer →").SetColor(tcell.ColorDarkGray)
	v.treeView = tview.NewTreeView().SetRoot(root).SetCurrentNode(root)
	v.treeView.SetBorder(true).SetTitle(" Connection Graph  <Tab> Switch Panel ")
	v.treeView.SetTopLevel(1)

	v.helpText = tview.NewTextView().
		SetDynamicColors(true).
		SetText("  [darkgray]← Load Balancer list  |  Connection graph →  |  [Tab] switch panel  [/] filter  [Enter] expand/collapse[-]")
	v.helpText.SetBackgroundColor(tcell.ColorDarkSlateGray)

	rightPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.treeView, 0, 1, false).
		AddItem(v.helpText, 1, 0, false)

	v.flex = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(v.lbList, 30, 0, true).
		AddItem(rightPane, 0, 1, false)

	v.pages.AddPage("main", v.flex, true, true)

	// Tab switches focus between list and tree; / opens filter on the LB list.
	v.lbList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(v.treeView)
			return nil
		}
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
	})
	v.treeView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(v.lbList)
			return nil
		}
		if event.Rune() == '/' {
			app.SetFocus(v.lbList)
			v.openFilter()
			return nil
		}
		return event
	})

	// Debounce: wait 300 ms after the last move before firing API calls.
	// Without this, every arrow-key press triggers a full buildTree which
	// makes several AWS API calls and causes visible lag.
	v.lbList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		v.debounceMu.Lock()
		if v.debounce != nil {
			v.debounce.Stop()
		}
		idx := index // capture
		v.debounce = time.AfterFunc(300*time.Millisecond, func() {
			if idx < len(v.lbs) {
				go v.buildTree(v.lbs[idx])
			}
		})
		v.debounceMu.Unlock()
	})

	return v
}

func (v *GraphView) GetContent() tview.Primitive  { return v.pages }
func (v *GraphView) GetFocusable() tview.Primitive { return v.lbList }

func (v *GraphView) openFilter() {
	prev := v.filter
	input := tview.NewInputField().
		SetLabel("  / Filter: ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
	input.SetChangedFunc(func(text string) {
		v.filter = text
		v.rebuildLBList()
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			v.filter = prev
			v.rebuildLBList()
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.lbList)
	})
	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.flex, 0, 1, false).
		AddItem(input, 1, 0, true)
	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
}

func (v *GraphView) rebuildLBList() {
	lf := strings.ToLower(v.filter)
	v.lbList.Clear()
	if v.filter != "" {
		v.lbList.SetTitle(fmt.Sprintf(" Load Balancers  /%s  </> Filter ", v.filter))
	} else {
		v.lbList.SetTitle(" Load Balancers  </> Filter ")
	}
	for _, lb := range v.lbs {
		if lf != "" && !strings.Contains(strings.ToLower(lb.Name), lf) {
			continue
		}
		icon := theme.StateIcon(lb.State)
		scheme := "int"
		if lb.Scheme == "internet-facing" {
			scheme = "ext"
		}
		v.lbList.AddItem(
			fmt.Sprintf(" %s %s", icon, lb.Name),
			fmt.Sprintf("   [%s] %s", scheme, lb.Type),
			0,
			nil,
		)
	}
}

func (v *GraphView) Refresh(ctx context.Context) {
	lbs, err := v.client.ListLoadBalancers(ctx)
	if err != nil {
		return
	}
	tgs, err := v.client.ListTargetGroups(ctx)
	if err == nil {
		for i := range tgs {
			v.tgCache[tgs[i].ARN] = tgs[i]
		}
	}
	v.lbs = lbs
	v.app.QueueUpdateDraw(func() {
		v.rebuildLBList()
		if len(lbs) > 0 {
			go v.buildTree(lbs[0])
		}
	})
}

var graphSpinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (v *GraphView) buildTree(lb aws.LoadBalancer) {
	ctx := context.Background()

	// Animate the tree title while fetching.
	spinStop := make(chan struct{})
	go func() {
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		frame := 0
		for {
			select {
			case <-spinStop:
				return
			case <-tick.C:
				f := graphSpinFrames[frame%len(graphSpinFrames)]
				frame++
				v.app.QueueUpdateDraw(func() {
					v.treeView.SetTitle(fmt.Sprintf(" %s Loading %s… ", f, lb.Name))
				})
			}
		}
	}()

	listeners, err := v.client.GetListeners(ctx, lb.ARN)
	close(spinStop)
	if err != nil {
		v.app.QueueUpdateDraw(func() {
			v.treeView.SetTitle(" Connection Graph  <Tab> Switch Panel ")
		})
		return
	}

	// Fetch rules for all listeners in parallel
	type listenerRules struct {
		listener aws.Listener
		rules    []aws.Rule
	}
	results := make([]listenerRules, len(listeners))
	var wg sync.WaitGroup
	for i, l := range listeners {
		wg.Add(1)
		go func(idx int, listener aws.Listener) {
			defer wg.Done()
			rules, _ := v.client.GetRules(ctx, listener.ARN)
			results[idx] = listenerRules{listener: listener, rules: rules}
		}(i, l)
	}
	wg.Wait()

	// Collect all unique TG ARNs we need health for (covers both single and multi-TG forwards).
	tgARNs := map[string]bool{}
	for _, lr := range results {
		for _, rule := range lr.rules {
			for _, action := range rule.Actions {
				if action.TargetGroupARN != "" {
					tgARNs[action.TargetGroupARN] = true
				}
				for _, ft := range action.ForwardTargets {
					if ft.ARN != "" {
						tgARNs[ft.ARN] = true
					}
				}
			}
		}
	}

	// Fetch health for all target groups in parallel
	healthMap := map[string][]aws.Target{}
	var mu sync.Mutex
	var hwg sync.WaitGroup
	for arn := range tgARNs {
		hwg.Add(1)
		go func(a string) {
			defer hwg.Done()
			targets, err := v.client.GetTargetHealth(ctx, a)
			if err == nil {
				mu.Lock()
				healthMap[a] = targets
				mu.Unlock()
			}
		}(arn)
	}
	hwg.Wait()

	// Build tree
	schemeLabel := "internal"
	schemeColor := tcell.ColorDarkGray
	if lb.Scheme == "internet-facing" {
		schemeLabel = "internet-facing"
		schemeColor = tcell.ColorAqua
	}
	lbStateColor := theme.StateColor(lb.State)

	root := tview.NewTreeNode(fmt.Sprintf(" ◈ %s", lb.Name)).
		SetColor(lbStateColor).
		SetExpanded(true)

	metaNode := tview.NewTreeNode(
		fmt.Sprintf("   %s  •  %s  •  %s", lb.Type, schemeLabel, lb.DNSName)).
		SetColor(schemeColor).
		SetSelectable(false)
	root.AddChild(metaNode)

	for _, lr := range results {
		l := lr.listener
		listenerLabel := fmt.Sprintf(" ─── :%d %s", l.Port, l.Protocol)
		if l.SSLPolicy != "" {
			listenerLabel += fmt.Sprintf("  [%s]", l.SSLPolicy)
		}
		listenerNode := tview.NewTreeNode(listenerLabel).
			SetColor(tcell.ColorWhite).
			SetExpanded(true)
		root.AddChild(listenerNode)

		for _, rule := range lr.rules {
			ruleLabel := buildRuleLabel(rule)
			ruleNode := tview.NewTreeNode(ruleLabel).
				SetColor(tcell.ColorDarkGray).
				SetExpanded(true)
			listenerNode.AddChild(ruleNode)

			for _, action := range rule.Actions {
				if action.Type != "forward" {
					ruleNode.AddChild(tview.NewTreeNode(buildActionLabel(action)).
						SetColor(tcell.ColorYellow).SetSelectable(false))
					continue
				}

				// Collect the target group ARNs for this action — either from
				// ForwardTargets (multi-TG weighted forward) or the simple single-ARN field.
				type tgEntry struct {
					arn    string
					weight int32
				}
				var tgEntries []tgEntry
				if len(action.ForwardTargets) > 0 {
					for _, ft := range action.ForwardTargets {
						tgEntries = append(tgEntries, tgEntry{arn: ft.ARN, weight: ft.Weight})
					}
				} else if action.TargetGroupARN != "" {
					tgEntries = append(tgEntries, tgEntry{arn: action.TargetGroupARN})
				}

				for _, entry := range tgEntries {
					targets := healthMap[entry.arn]
					healthy, total := countHealth(targets)
					tgName := entry.arn
					if tg, ok := v.tgCache[entry.arn]; ok {
						tgName = tg.Name
					}
					tgColor := tcell.ColorGreen
					if total > 0 && healthy < total {
						tgColor = tcell.ColorYellow
					}
					if total > 0 && healthy == 0 {
						tgColor = tcell.ColorRed
					}
					w := ""
					if entry.weight > 0 {
						w = fmt.Sprintf(" (weight: %d%%)", entry.weight)
					}
					tgLabel := fmt.Sprintf("   ▶ %s  %d/%d %s%s",
						tgName, healthy, total, theme.HealthBar(healthy, total), w)
					tgNode := tview.NewTreeNode(tgLabel).
						SetColor(tgColor).
						SetExpanded(true)
					ruleNode.AddChild(tgNode)

					for _, t := range targets {
						tc := theme.StateColor(t.Health)
						icon := theme.StateIcon(t.Health)
						port := ""
						if t.Port > 0 {
							port = fmt.Sprintf(":%d", t.Port)
						}
						desc := ""
						if t.HealthDesc != "" {
							desc = fmt.Sprintf("  %s", t.HealthDesc)
						}
						tgNode.AddChild(tview.NewTreeNode(
							fmt.Sprintf("      %s %s%s%s", icon, t.ID, port, desc)).
							SetColor(tc).SetSelectable(false))
					}
				}
			}
		}
	}

	v.app.QueueUpdateDraw(func() {
		v.treeView.SetTitle(" Connection Graph  <Tab> Switch Panel ")
		v.treeView.SetRoot(root).SetCurrentNode(root)
	})
}

func buildRuleLabel(rule aws.Rule) string {
	pri := rule.Priority
	if rule.IsDefault {
		pri = "default"
	}
	if len(rule.Conditions) == 0 {
		return fmt.Sprintf("     [priority: %s]", pri)
	}
	var b strings.Builder
	for _, c := range rule.Conditions {
		if len(c.Values) > 0 {
			fmt.Fprintf(&b, "%s=%s  ", c.Field, c.Values[0])
		}
	}
	return fmt.Sprintf("     [%s]  %s", pri, b.String())
}

func buildActionLabel(action aws.RuleAction) string {
	switch action.Type {
	case "redirect":
		if action.RedirectConfig != nil {
			return fmt.Sprintf("   ↪ redirect → %s://%s  %s",
				action.RedirectConfig.Protocol,
				action.RedirectConfig.Port,
				action.RedirectConfig.StatusCode)
		}
		return "   ↪ redirect"
	case "fixed-response":
		return "   ✖ fixed-response"
	default:
		return fmt.Sprintf("   %s", action.Type)
	}
}
