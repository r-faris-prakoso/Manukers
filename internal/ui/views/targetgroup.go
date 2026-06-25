package views

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/theme"
)

// TGView shows target groups with real-time health monitoring.
type TGView struct {
	app    *tview.Application
	client *aws.Client

	pages   *tview.Pages
	tgTable *tview.Table
	tgs     []aws.TargetGroup

	// health monitor state
	monitoring  int32 // atomic: 1 when detail is open
	monitorStop chan struct{}
}

func NewTGView(app *tview.Application, client *aws.Client) *TGView {
	v := &TGView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.tgTable = newStyledTable(" Target Groups  <Enter> Health Monitor ")
	v.tgTable.SetSelectedFunc(func(row, col int) {
		if row > 0 && row <= len(v.tgs) {
			v.openHealthMonitor(v.tgs[row-1])
		}
	})
	v.pages.AddPage("list", v.tgTable, true, true)
	return v
}

func (v *TGView) GetContent() tview.Primitive  { return v.pages }
func (v *TGView) GetFocusable() tview.Primitive { return v.tgTable }

func (v *TGView) Refresh(ctx context.Context) {
	showLoading(v.app, v.tgTable)
	tgs, err := v.client.ListTargetGroups(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.tgTable, err.Error()) })
		return
	}

	// Fetch target health for each TG to show health counts
	for i := range tgs {
		targets, err := v.client.GetTargetHealth(ctx, tgs[i].ARN)
		if err == nil {
			tgs[i].Targets = targets
		}
	}

	v.tgs = tgs
	v.app.QueueUpdateDraw(func() { v.updateTGTable() })
}

func (v *TGView) updateTGTable() {
	v.tgTable.Clear()
	for col, h := range []string{"NAME", "PROTOCOL:PORT", "TYPE", "VPC", "HEALTH", "LOAD BALANCERS"} {
		v.tgTable.SetCell(0, col, headerCell(h))
	}

	for i, tg := range v.tgs {
		row := i + 1
		healthy, total := countHealth(tg.Targets)
		hColor := tcell.ColorGreen
		if total > 0 && healthy < total {
			hColor = tcell.ColorYellow
		}
		if total > 0 && healthy == 0 {
			hColor = tcell.ColorRed
		}

		protoPort := fmt.Sprintf("%s:%d", tg.Protocol, tg.Port)
		lbCount := len(tg.LoadBalancers)
		lbStr := "─"
		if lbCount > 0 {
			lbStr = fmt.Sprintf("%d attached", lbCount)
		}

		v.tgTable.SetCell(row, 0, tview.NewTableCell(" "+tg.Name).SetTextColor(tcell.ColorWhite))
		v.tgTable.SetCell(row, 1, tview.NewTableCell(" "+protoPort).SetTextColor(tcell.ColorAqua))
		v.tgTable.SetCell(row, 2, tview.NewTableCell(" "+tg.TargetType).SetTextColor(tcell.ColorDarkGray))
		v.tgTable.SetCell(row, 3, tview.NewTableCell(" "+orDash(tg.VpcID)).SetTextColor(tcell.ColorDarkGray))
		v.tgTable.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("  %d/%d %s", healthy, total, theme.HealthBar(healthy, total))).
			SetTextColor(hColor))
		v.tgTable.SetCell(row, 5, tview.NewTableCell(" "+lbStr).SetTextColor(tcell.ColorDarkGray))
	}
	if len(v.tgs) == 0 {
		v.tgTable.SetCell(1, 0, tview.NewTableCell("  No target groups found").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *TGView) openHealthMonitor(tg aws.TargetGroup) {
	// Stop any previous monitor
	v.stopMonitor()

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	tv.SetBorder(true).
		SetTitle(fmt.Sprintf(" Health Monitor: %s  <Esc> Back  auto-refresh: 10s ", tg.Name))

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			v.stopMonitor()
			v.pages.SwitchToPage("list")
			v.app.SetFocus(v.tgTable)
			return nil
		}
		return event
	})

	v.pages.AddAndSwitchToPage("health", tv, true)
	v.app.SetFocus(tv)

	// Start live health monitor goroutine
	atomic.StoreInt32(&v.monitoring, 1)
	v.monitorStop = make(chan struct{})

	updateHealth := func() {
		ctx := context.Background()
		targets, err := v.client.GetTargetHealth(ctx, tg.ARN)
		if err != nil {
			v.app.QueueUpdateDraw(func() {
				tv.SetText(fmt.Sprintf("[red]Error fetching health: %s[-]", err.Error()))
			})
			return
		}
		text := buildHealthText(tg, targets)
		v.app.QueueUpdateDraw(func() {
			tv.SetText(text)
		})
	}

	// Initial fetch
	go updateHealth()

	// Periodic refresh every 10s
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if atomic.LoadInt32(&v.monitoring) == 0 {
					return
				}
				updateHealth()
			case <-v.monitorStop:
				return
			}
		}
	}()
}

func (v *TGView) stopMonitor() {
	if atomic.LoadInt32(&v.monitoring) == 1 {
		atomic.StoreInt32(&v.monitoring, 0)
		if v.monitorStop != nil {
			close(v.monitorStop)
			v.monitorStop = nil
		}
	}
}

func buildHealthText(tg aws.TargetGroup, targets []aws.Target) string {
	healthy, total := countHealth(targets)

	overallColor := "green"
	if total > 0 && healthy < total {
		overallColor = "yellow"
	}
	if total > 0 && healthy == 0 {
		overallColor = "red"
	}

	bar := theme.HealthBar(healthy, total)

	text := fmt.Sprintf("[aqua::b]  %s[-:-:-]\n\n", tg.Name)
	text += fmt.Sprintf("  [yellow]ARN[-]          [darkgray]%s[-]\n", tg.ARN)
	text += fmt.Sprintf("  [yellow]Protocol:Port[-] [white]%s:%d[-]\n", tg.Protocol, tg.Port)
	text += fmt.Sprintf("  [yellow]Target Type[-]  [white]%s[-]\n", tg.TargetType)
	text += fmt.Sprintf("  [yellow]VPC[-]          [white]%s[-]\n\n", orDash(tg.VpcID))

	text += fmt.Sprintf("  [yellow::b]Health Status[-:-:-]  [%s]%d/%d[-]  %s\n\n", overallColor, healthy, total, bar)

	if tg.HealthCheck.Protocol != "" {
		text += "  [yellow::b]Health Check Config[-:-:-]\n"
		text += fmt.Sprintf("  [darkgray]  Protocol  [-][white]%s[-]\n", tg.HealthCheck.Protocol)
		if tg.HealthCheck.Path != "" {
			text += fmt.Sprintf("  [darkgray]  Path      [-][white]%s[-]\n", tg.HealthCheck.Path)
		}
		text += fmt.Sprintf("  [darkgray]  Threshold [-][white]%d healthy / %d unhealthy[-]\n",
			tg.HealthCheck.HealthyThreshold, tg.HealthCheck.UnhealthyThreshold)
		text += fmt.Sprintf("  [darkgray]  Interval  [-][white]%ds (timeout %ds)[-]\n\n",
			tg.HealthCheck.Interval, tg.HealthCheck.Timeout)
	}

	text += "  [yellow::b]Targets[-:-:-]\n"
	if len(targets) == 0 {
		text += "  [darkgray]No targets registered[-]\n"
	}
	for _, t := range targets {
		hc := theme.StateColorName(t.Health)
		icon := theme.StateIcon(t.Health)
		port := ""
		if t.Port > 0 {
			port = fmt.Sprintf(":%d", t.Port)
		}
		az := ""
		if t.AZ != "" {
			az = fmt.Sprintf(" [darkgray](%s)[-]", t.AZ)
		}
		desc := ""
		if t.HealthDesc != "" {
			desc = fmt.Sprintf("  [darkgray]%s[-]", t.HealthDesc)
		}
		text += fmt.Sprintf("  [%s]%s[-]  [white]%s%s[-]%s%s\n",
			hc, icon, t.ID, port, az, desc)
	}

	text += fmt.Sprintf("\n  [darkgray]Last updated: %s  Auto-refresh: 10s  [Esc] Back[-]",
		time.Now().Format("15:04:05"))
	return text
}

func countHealth(targets []aws.Target) (healthy, total int) {
	total = len(targets)
	for _, t := range targets {
		if t.Health == "healthy" {
			healthy++
		}
	}
	return
}
