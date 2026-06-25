package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/theme"
)

// LBView shows load balancers with drill-down into listeners → rules → targets.
type LBView struct {
	app    *tview.Application
	client *aws.Client

	pages          *tview.Pages
	lbTable        *tview.Table
	listenersTable *tview.Table // stored so openRules can focus it on Esc
	lbs            []aws.LoadBalancer
	filter         string
}

func NewLBView(app *tview.Application, client *aws.Client) *LBView {
	v := &LBView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.lbTable = newStyledTable(" Load Balancers  <Enter> Listeners  </> Filter ")
	// Use cell reference so selection works correctly even when rows are filtered.
	v.lbTable.SetSelectedFunc(func(row, col int) {
		cell := v.lbTable.GetCell(row, 0)
		if cell == nil || cell.GetReference() == nil {
			return
		}
		v.openListeners(*cell.GetReference().(*aws.LoadBalancer))
	})
	v.lbTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
	})
	v.pages.AddPage("list", v.lbTable, true, true)
	return v
}

func (v *LBView) GetContent() tview.Primitive  { return v.pages }
func (v *LBView) GetFocusable() tview.Primitive { return v.lbTable }

func (v *LBView) Refresh(ctx context.Context) {
	showLoading(v.app, v.lbTable)
	lbs, err := v.client.ListLoadBalancers(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.lbTable, err.Error()) })
		return
	}
	v.lbs = lbs
	v.app.QueueUpdateDraw(func() { v.updateLBTable() })
}

func (v *LBView) updateLBTable() {
	v.lbTable.Clear()
	if v.filter != "" {
		v.lbTable.SetTitle(fmt.Sprintf(" Load Balancers  /%s  </> Filter ", v.filter))
	} else {
		v.lbTable.SetTitle(" Load Balancers  <Enter> Listeners  </> Filter ")
	}
	for col, h := range []string{"NAME", "TYPE", "STATE", "SCHEME", "VPC", "DNS NAME"} {
		v.lbTable.SetCell(0, col, headerCell(h))
	}
	row := 1
	for i := range v.lbs {
		lb := v.lbs[i]
		if v.filter != "" && !strings.Contains(strings.ToLower(lb.Name), strings.ToLower(v.filter)) {
			continue
		}
		sc := theme.StateColor(lb.State)
		icon := theme.StateIcon(lb.State)
		// Store a pointer to the LB in the first cell so SetSelectedFunc can
		// retrieve the correct item regardless of how many rows are filtered.
		v.lbTable.SetCell(row, 0, tview.NewTableCell(" "+lb.Name).
			SetTextColor(tcell.ColorWhite).SetReference(&v.lbs[i]))
		v.lbTable.SetCell(row, 1, tview.NewTableCell(" "+lb.Type).SetTextColor(tcell.ColorDarkGray))
		v.lbTable.SetCell(row, 2, tview.NewTableCell(" "+icon+" "+lb.State).SetTextColor(sc))
		v.lbTable.SetCell(row, 3, tview.NewTableCell(" "+lb.Scheme).SetTextColor(schemeColor(lb.Scheme)))
		v.lbTable.SetCell(row, 4, tview.NewTableCell(" "+lb.VpcID).SetTextColor(tcell.ColorDarkGray))
		v.lbTable.SetCell(row, 5, tview.NewTableCell(" "+lb.DNSName).SetTextColor(tcell.ColorDarkGray).SetMaxWidth(50))
		row++
	}
	if row == 1 {
		msg := "  No load balancers found"
		if v.filter != "" {
			msg = fmt.Sprintf("  No results for \"%s\"  [Esc to clear]", v.filter)
		}
		v.lbTable.SetCell(1, 0, tview.NewTableCell(msg).SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *LBView) openFilter() {
	prev := v.filter
	input := tview.NewInputField().
		SetLabel("  / Filter: ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
	input.SetChangedFunc(func(text string) {
		v.filter = text
		v.updateLBTable()
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			v.filter = prev // Esc reverts to whatever was active before
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.lbTable)
		v.updateLBTable()
	})
	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.lbTable, 0, 1, false).
		AddItem(input, 1, 0, true)
	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
}

// openListeners shows a loading page instantly, then fetches in the background.
// This must never block the main event loop.
func (v *LBView) openListeners(lb aws.LoadBalancer) {
	// Step 1: show loading page with zero API calls — instant.
	loading := loadingText(fmt.Sprintf(" Listeners: %s ", lb.Name))
	loading.SetInputCapture(escBack(v.app, v.pages, "list", v.lbTable))
	v.pages.AddAndSwitchToPage("listeners", loading, true)
	v.app.SetFocus(loading)

	// Step 2: fetch in background goroutine.
	go func() {
		ctx := context.Background()
		listeners, err := v.client.GetListeners(ctx, lb.ARN)
		if err != nil {
			v.app.QueueUpdateDraw(func() {
				loading.SetText(fmt.Sprintf("  [red]Error: %s[-]", err))
			})
			return
		}

		// Build the real table (tview primitive creation is goroutine-safe
		// as long as the primitive isn't yet attached to the screen).
		table := newStyledTable(fmt.Sprintf(" Listeners: %s  <Enter> Rules  <Esc> Back ", lb.Name))
		table.SetInputCapture(escBack(v.app, v.pages, "list", v.lbTable))
		table.SetSelectedFunc(func(row, col int) {
			if row > 0 && row <= len(listeners) {
				// openRules also shows loading instantly.
				v.openRules(lb, listeners[row-1])
			}
		})
		for col, h := range []string{"PORT", "PROTOCOL", "SSL POLICY"} {
			table.SetCell(0, col, headerCell(h))
		}
		for i, l := range listeners {
			row := i + 1
			table.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf(" %d", l.Port)).SetTextColor(tcell.ColorAqua))
			table.SetCell(row, 1, tview.NewTableCell(" "+l.Protocol).SetTextColor(tcell.ColorWhite))
			table.SetCell(row, 2, tview.NewTableCell(" "+orDash(l.SSLPolicy)).SetTextColor(tcell.ColorDarkGray))
		}
		if len(listeners) == 0 {
			table.SetCell(1, 0, tview.NewTableCell("  No listeners found").
				SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
		}

		// Step 3: swap loading page → real table on the main loop.
		// Also save the table so openRules can restore focus on Esc.
		v.app.QueueUpdateDraw(func() {
			v.listenersTable = table
			v.pages.AddAndSwitchToPage("listeners", table, true)
			v.app.SetFocus(table)
		})
	}()
}

// openRules shows a loading page instantly, then fetches in the background.
// Called from SetSelectedFunc which runs on the main loop — never use
// QueueUpdateDraw here for the initial setup; call tview methods directly.
func (v *LBView) openRules(lb aws.LoadBalancer, listener aws.Listener) {
	// Step 1: instant loading page — direct calls, we are on the main loop.
	baseTitle := fmt.Sprintf(" Rules: %s :%d ", lb.Name, listener.Port)
	loading := loadingText(baseTitle)
	loading.SetInputCapture(escBack(v.app, v.pages, "listeners", v.listenersTable))
	v.pages.AddAndSwitchToPage("rules", loading, true)
	v.app.SetFocus(loading)

	// Step 2: fetch in background.
	go func() {
		ctx := context.Background()
		rules, err := v.client.GetRules(ctx, listener.ARN)
		if err != nil {
			v.app.QueueUpdateDraw(func() {
				loading.SetText(fmt.Sprintf("  [red]Error: %s[-]", err))
			})
			return
		}

		v.app.QueueUpdateDraw(func() {
			doneTitle := fmt.Sprintf(" Rules: %s :%d  </> Filter  <Esc> Back ", lb.Name, listener.Port)
			loading.SetTitle(doneTitle)
			loading.SetText(buildRulesText(lb, listener, rules, ""))

			var rulesFilter string
			loading.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyEscape {
					v.pages.SwitchToPage("listeners")
					if v.listenersTable != nil {
						v.app.SetFocus(v.listenersTable)
					}
					return nil
				}
				if event.Rune() == '/' {
					prev := rulesFilter
					input := tview.NewInputField().
						SetLabel("  / Filter: ").
						SetFieldWidth(30).
						SetText(rulesFilter).
						SetFieldTextColor(tcell.ColorWhite).
						SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
					input.SetChangedFunc(func(text string) {
						rulesFilter = text
						if rulesFilter != "" {
							loading.SetTitle(fmt.Sprintf(" Rules: %s :%d  /%s  </> Filter  <Esc> Back ", lb.Name, listener.Port, rulesFilter))
						} else {
							loading.SetTitle(doneTitle)
						}
						loading.SetText(buildRulesText(lb, listener, rules, rulesFilter))
					})
					input.SetDoneFunc(func(key tcell.Key) {
						if key != tcell.KeyEnter {
							rulesFilter = prev
							loading.SetText(buildRulesText(lb, listener, rules, rulesFilter))
						}
						if rulesFilter != "" {
							loading.SetTitle(fmt.Sprintf(" Rules: %s :%d  /%s  </> Filter  <Esc> Back ", lb.Name, listener.Port, rulesFilter))
						} else {
							loading.SetTitle(doneTitle)
						}
						v.pages.RemovePage("rulesFilter")
						v.app.SetFocus(loading)
					})
					filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(loading, 0, 1, false).
						AddItem(input, 1, 0, true)
					v.pages.AddAndSwitchToPage("rulesFilter", filterLayout, true)
					v.app.SetFocus(input)
					return nil
				}
				return event
			})
		})
	}()
}

// ─── Text builders ────────────────────────────────────────────────────────────

func buildRulesText(lb aws.LoadBalancer, listener aws.Listener, rules []aws.Rule, filter string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[aqua::b]%s  Port %d / %s[-:-:-]\n\n", lb.Name, listener.Port, listener.Protocol)
	if listener.SSLPolicy != "" {
		fmt.Fprintf(&b, "[darkgray]SSL Policy: %s[-]\n\n", listener.SSLPolicy)
	}
	lf := strings.ToLower(filter)
	matched := 0
	for _, rule := range rules {
		pri := rule.Priority
		if rule.IsDefault {
			pri = "default"
		}
		if filter != "" {
			var hb strings.Builder
			hb.WriteString(strings.ToLower(pri))
			for _, cond := range rule.Conditions {
				hb.WriteByte(' ')
				hb.WriteString(strings.ToLower(cond.Field))
				hb.WriteByte(' ')
				hb.WriteString(strings.ToLower(strings.Join(cond.Values, " ")))
			}
			for _, action := range rule.Actions {
				hb.WriteByte(' ')
				hb.WriteString(strings.ToLower(action.Type))
				hb.WriteByte(' ')
				hb.WriteString(strings.ToLower(action.TargetGroupARN))
				for _, ft := range action.ForwardTargets {
					hb.WriteByte(' ')
					hb.WriteString(strings.ToLower(ft.ARN))
				}
			}
			if !strings.Contains(hb.String(), lf) {
				continue
			}
		}
		matched++
		fmt.Fprintf(&b, "[yellow::b]Priority: %s[-:-:-]\n", pri)
		if len(rule.Conditions) > 0 {
			b.WriteString("  [darkgray]Conditions:[-]\n")
			for _, cond := range rule.Conditions {
				fmt.Fprintf(&b, "    [white]%s[-] [darkgray]=[-] [aqua]%s[-]\n",
					cond.Field, strings.Join(cond.Values, ", "))
			}
		}
		if len(rule.Actions) > 0 {
			b.WriteString("  [darkgray]Actions:[-]\n")
			for _, action := range rule.Actions {
				switch action.Type {
				case "forward":
					if len(action.ForwardTargets) > 0 {
						b.WriteString("    [green]→ forward[-]\n")
						for _, ft := range action.ForwardTargets {
							if ft.Weight > 0 {
								fmt.Fprintf(&b, "      [white]%s[-]  [darkgray](weight: %d)[-]\n", ft.ARN, ft.Weight)
							} else {
								fmt.Fprintf(&b, "      [white]%s[-]\n", ft.ARN)
							}
						}
					} else {
						fmt.Fprintf(&b, "    [green]→ forward[-] [white]%s[-]\n", action.TargetGroupARN)
					}
				case "redirect":
					if action.RedirectConfig != nil {
						fmt.Fprintf(&b, "    [yellow]→ redirect[-] [white]%s://%s  %s[-]\n",
							action.RedirectConfig.Protocol,
							action.RedirectConfig.Port,
							action.RedirectConfig.StatusCode)
					}
				case "fixed-response":
					b.WriteString("    [red]→ fixed-response[-]\n")
				default:
					fmt.Fprintf(&b, "    [darkgray]→ %s[-]\n", action.Type)
				}
			}
		}
		b.WriteByte('\n')
	}
	if matched == 0 {
		if filter != "" {
			fmt.Fprintf(&b, "  [darkgray]No rules match \"%s\"[-]\n", filter)
		} else {
			b.WriteString("  [darkgray]No rules found[-]\n")
		}
	}
	return b.String()
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// loadingText returns a styled TextView showing "Loading…" that can be swapped
// out for the real content once the background fetch completes.
func loadingText(title string) *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	tv.SetBorder(true).SetTitle(title)
	tv.SetText("  [darkgray]Loading…[-]")
	return tv
}

func newStyledTable(title string) *tview.Table {
	t := tview.NewTable()
	t.SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorder(true).
		SetTitle(title)
	t.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite))
	return t
}

func headerCell(label string) *tview.TableCell {
	return tview.NewTableCell(" " + label + " ").
		SetTextColor(tcell.ColorYellow).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false)
}

func showLoading(app *tview.Application, table *tview.Table) {
	app.QueueUpdateDraw(func() {
		table.Clear()
		table.SetCell(1, 0, tview.NewTableCell("  Loading…").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	})
}

func showTableError(table *tview.Table, msg string) {
	table.Clear()
	table.SetCell(1, 0, tview.NewTableCell("  Error: "+msg).
		SetTextColor(tcell.ColorRed).SetSelectable(false))
}

func escBack(app *tview.Application, pages *tview.Pages, backPage string, focus tview.Primitive) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(backPage)
			if focus != nil {
				app.SetFocus(focus)
			}
			return nil
		}
		return event
	}
}

func schemeColor(scheme string) tcell.Color {
	if scheme == "internet-facing" {
		return tcell.ColorAqua
	}
	return tcell.ColorDarkGray
}
