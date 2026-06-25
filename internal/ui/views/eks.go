package views

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/theme"
)

// EKSView shows EKS clusters with drill-down into node groups and add-ons.
type EKSView struct {
	app    *tview.Application
	client *aws.Client

	pages    *tview.Pages
	eksTable *tview.Table
	clusters []aws.EKSCluster
	filter   string
}

func NewEKSView(app *tview.Application, client *aws.Client) *EKSView {
	v := &EKSView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.eksTable = newStyledTable(" EKS Clusters  <Enter> Details  </> Filter ")
	v.eksTable.SetSelectedFunc(func(row, col int) {
		cell := v.eksTable.GetCell(row, 0)
		if cell == nil || cell.GetReference() == nil {
			return
		}
		v.openClusterDetail(*cell.GetReference().(*aws.EKSCluster))
	})
	v.eksTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
	})
	v.pages.AddPage("list", v.eksTable, true, true)
	return v
}

func (v *EKSView) GetContent() tview.Primitive  { return v.pages }
func (v *EKSView) GetFocusable() tview.Primitive { return v.eksTable }

func (v *EKSView) Refresh(ctx context.Context) {
	showLoading(v.app, v.eksTable)
	clusters, err := v.client.ListClusters(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.eksTable, err.Error()) })
		return
	}
	v.clusters = clusters
	v.app.QueueUpdateDraw(func() { v.updateEKSTable() })
}

func (v *EKSView) updateEKSTable() {
	v.eksTable.Clear()
	if v.filter != "" {
		v.eksTable.SetTitle(fmt.Sprintf(" EKS Clusters  /%s  </> Filter ", v.filter))
	} else {
		v.eksTable.SetTitle(" EKS Clusters  <Enter> Details  </> Filter ")
	}
	for col, h := range []string{"NAME", "STATUS", "VERSION", "VPC", "ENDPOINT"} {
		v.eksTable.SetCell(0, col, headerCell(h))
	}
	row := 1
	for i := range v.clusters {
		cl := v.clusters[i]
		if v.filter != "" && !strings.Contains(strings.ToLower(cl.Name), strings.ToLower(v.filter)) {
			continue
		}
		sc := theme.StateColor(cl.Status)
		icon := theme.StateIcon(cl.Status)
		ep := cl.Endpoint
		if len(ep) > 45 {
			ep = ep[:42] + "…"
		}
		v.eksTable.SetCell(row, 0, tview.NewTableCell(" "+cl.Name).
			SetTextColor(tcell.ColorWhite).SetReference(&v.clusters[i]))
		v.eksTable.SetCell(row, 1, tview.NewTableCell(" "+icon+" "+cl.Status).SetTextColor(sc))
		v.eksTable.SetCell(row, 2, tview.NewTableCell(" "+cl.Version).SetTextColor(tcell.ColorAqua))
		v.eksTable.SetCell(row, 3, tview.NewTableCell(" "+orDash(cl.VpcID)).SetTextColor(tcell.ColorDarkGray))
		v.eksTable.SetCell(row, 4, tview.NewTableCell(" "+orDash(ep)).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	if row == 1 {
		msg := "  No EKS clusters found"
		if v.filter != "" {
			msg = fmt.Sprintf("  No results for \"%s\"  [Esc to clear]", v.filter)
		}
		v.eksTable.SetCell(1, 0, tview.NewTableCell(msg).SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *EKSView) openFilter() {
	prev := v.filter
	input := tview.NewInputField().
		SetLabel("  / Filter: ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
	input.SetChangedFunc(func(text string) {
		v.filter = text
		v.updateEKSTable()
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			v.filter = prev // Esc reverts to whatever was active before
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.eksTable)
		v.updateEKSTable()
	})
	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.eksTable, 0, 1, false).
		AddItem(input, 1, 0, true)
	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
}

func (v *EKSView) openClusterDetail(cl aws.EKSCluster) {
	// Step 1: show loading page instantly — no blocking.
	loading := loadingText(fmt.Sprintf(" Cluster: %s ", cl.Name))
	loading.SetInputCapture(escBack(v.app, v.pages, "list", v.eksTable))
	v.pages.AddAndSwitchToPage("detail", loading, true)
	v.app.SetFocus(loading)

	// Step 2: fetch node groups + add-ons concurrently without blocking the main loop.
	go func() {
		ctx := context.Background()
		var (
			nodeGroups []aws.NodeGroup
			addons     []aws.Addon
			wg         sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); nodeGroups, _ = v.client.ListNodeGroups(ctx, cl.Name) }()
		go func() { defer wg.Done(); addons, _ = v.client.ListAddons(ctx, cl.Name) }()
		wg.Wait()

		text := buildClusterText(cl, nodeGroups, addons)
		v.app.QueueUpdateDraw(func() {
			loading.SetTitle(fmt.Sprintf(" Cluster: %s  <Esc> Back ", cl.Name))
			loading.SetText(text)
		})
	}()
}

func buildClusterText(cl aws.EKSCluster, nodeGroups []aws.NodeGroup, addons []aws.Addon) string {
	sc := theme.StateColorName(cl.Status)

	text := fmt.Sprintf("[aqua::b]  %s[-:-:-]\n\n", cl.Name)
	text += fmt.Sprintf("  [yellow]Status   [-][%s]%s %s[-]\n", sc, theme.StateIcon(cl.Status), cl.Status)
	text += fmt.Sprintf("  [yellow]Version  [-][white]%s[-]\n", cl.Version)
	text += fmt.Sprintf("  [yellow]VPC      [-][white]%s[-]\n", orDash(cl.VpcID))
	text += fmt.Sprintf("  [yellow]Role ARN [-][darkgray]%s[-]\n", cl.RoleARN)
	if cl.Endpoint != "" {
		text += fmt.Sprintf("  [yellow]Endpoint [-][darkgray]%s[-]\n", cl.Endpoint)
	}
	if !cl.CreatedAt.IsZero() {
		text += fmt.Sprintf("  [yellow]Created  [-][white]%s[-]\n", cl.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	if len(cl.Tags) > 0 {
		text += "\n  [yellow::b]Tags[-:-:-]\n"
		for k, val := range cl.Tags {
			text += fmt.Sprintf("  [darkgray]  %-24s[-] [white]%s[-]\n", k, val)
		}
	}

	text += "\n  [yellow::b]Node Groups[-:-:-]\n"
	if len(nodeGroups) == 0 {
		text += "  [darkgray]  (none)[-]\n"
	}
	for _, ng := range nodeGroups {
		ngColor := theme.StateColorName(ng.Status)
		types := strings.Join(ng.InstanceTypes, ", ")
		text += fmt.Sprintf("  [%s]%s[-]  [white]%-30s[-]  [darkgray]%s[-]  "+
			"[white]desired:%d min:%d max:%d[-]\n",
			ngColor, theme.StateIcon(ng.Status), ng.Name, types,
			ng.DesiredSize, ng.MinSize, ng.MaxSize)
	}

	text += "\n  [yellow::b]Add-ons[-:-:-]\n"
	if len(addons) == 0 {
		text += "  [darkgray]  (none)[-]\n"
	}
	for _, a := range addons {
		ac := theme.StateColorName(a.Status)
		text += fmt.Sprintf("  [%s]%s[-]  [white]%-30s[-]  [darkgray]v%s[-]\n",
			ac, theme.StateIcon(a.Status), a.Name, a.Version)
	}

	text += "\n  [darkgray][Esc] Back[-]"
	return text
}
