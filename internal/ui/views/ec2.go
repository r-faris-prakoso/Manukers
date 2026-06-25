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

// EC2View shows EC2 instances with drill-down detail.
type EC2View struct {
	app    *tview.Application
	client *aws.Client

	pages     *tview.Pages
	table     *tview.Table
	detail    *tview.TextView
	instances []aws.Instance
	filter    string
}

func NewEC2View(app *tview.Application, client *aws.Client) *EC2View {
	v := &EC2View{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
		table:  tview.NewTable(),
		detail: tview.NewTextView().SetDynamicColors(true).SetScrollable(true),
	}

	v.table.
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorder(true).
		SetTitle(" EC2 Instances  <Enter> Detail  </> Filter ")
	v.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite))

	v.detail.SetBorder(true).SetTitle(" Instance Detail  <Esc> Back ")

	v.table.SetSelectedFunc(func(row, col int) {
		if row > 0 {
			v.showDetail(row - 1)
		}
	})

	v.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
	})

	v.pages.AddPage("list", v.table, true, true)
	return v
}

func (v *EC2View) GetContent() tview.Primitive  { return v.pages }
func (v *EC2View) GetFocusable() tview.Primitive { return v.table }

func (v *EC2View) Refresh(ctx context.Context) {
	v.app.QueueUpdateDraw(func() {
		v.table.Clear()
		v.table.SetCell(1, 0, tview.NewTableCell("  Loading…").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	})
	instances, err := v.client.ListInstances(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { v.showTableError(err.Error()) })
		return
	}
	v.instances = instances
	v.app.QueueUpdateDraw(func() { v.updateTable() })
}

func (v *EC2View) updateTable() {
	v.table.Clear()
	headers := []string{"NAME", "INSTANCE ID", "STATE", "TYPE", "PRIVATE IP", "PUBLIC IP", "AZ", "LAUNCHED"}
	for col, h := range headers {
		v.table.SetCell(0, col, tview.NewTableCell(" "+h+" ").
			SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false))
	}

	row := 1
	for _, inst := range v.instances {
		if v.filter != "" &&
			!strings.Contains(strings.ToLower(inst.Name), strings.ToLower(v.filter)) &&
			!strings.Contains(strings.ToLower(inst.ID), strings.ToLower(v.filter)) {
			continue
		}
		col := tcell.ColorWhite
		icon := theme.StateIcon(inst.State)
		sc := theme.StateColor(inst.State)

		launched := "─"
		if !inst.LaunchTime.IsZero() {
			launched = inst.LaunchTime.Format("2006-01-02")
		}
		_ = col
		v.table.SetCell(row, 0, tview.NewTableCell(" "+inst.Name).SetTextColor(tcell.ColorWhite))
		v.table.SetCell(row, 1, tview.NewTableCell(" "+inst.ID).SetTextColor(tcell.ColorAqua))
		v.table.SetCell(row, 2, tview.NewTableCell(" "+icon+" "+inst.State).SetTextColor(sc))
		v.table.SetCell(row, 3, tview.NewTableCell(" "+inst.Type).SetTextColor(tcell.ColorWhite))
		v.table.SetCell(row, 4, tview.NewTableCell(" "+inst.PrivateIP).SetTextColor(tcell.ColorWhite))
		v.table.SetCell(row, 5, tview.NewTableCell(" "+orDash(inst.PublicIP)).SetTextColor(tcell.ColorDarkGray))
		v.table.SetCell(row, 6, tview.NewTableCell(" "+inst.AZ).SetTextColor(tcell.ColorDarkGray))
		v.table.SetCell(row, 7, tview.NewTableCell(" "+launched).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	if row == 1 {
		v.table.SetCell(1, 0, tview.NewTableCell("  No instances found").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *EC2View) showDetail(idx int) {
	if idx < 0 || idx >= len(v.instances) {
		return
	}
	inst := v.instances[idx]

	sc := theme.StateColorName(inst.State)
	text := fmt.Sprintf("[aqua::b]  %s[-:-:-]\n\n", inst.Name)
	text += fmt.Sprintf("  [yellow]Instance ID  [-][white]%s[-]\n", inst.ID)
	text += fmt.Sprintf("  [yellow]State        [-][%s]%s %s[-]\n", sc, theme.StateIcon(inst.State), inst.State)
	text += fmt.Sprintf("  [yellow]Type         [-][white]%s[-]\n", inst.Type)
	text += fmt.Sprintf("  [yellow]Private IP   [-][white]%s[-]\n", inst.PrivateIP)
	if inst.PublicIP != "" {
		text += fmt.Sprintf("  [yellow]Public IP    [-][white]%s[-]\n", inst.PublicIP)
	}
	text += fmt.Sprintf("  [yellow]VPC          [-][white]%s[-]\n", inst.VpcID)
	text += fmt.Sprintf("  [yellow]Subnet       [-][white]%s[-]\n", inst.SubnetID)
	text += fmt.Sprintf("  [yellow]AZ           [-][white]%s[-]\n", inst.AZ)
	text += fmt.Sprintf("  [yellow]Image        [-][white]%s[-]\n", inst.ImageID)
	if inst.KeyName != "" {
		text += fmt.Sprintf("  [yellow]Key Name     [-][white]%s[-]\n", inst.KeyName)
	}
	if !inst.LaunchTime.IsZero() {
		text += fmt.Sprintf("  [yellow]Launched     [-][white]%s[-]\n", inst.LaunchTime.Format("2006-01-02 15:04:05"))
	}

	if len(inst.Tags) > 0 {
		text += "\n  [yellow::b]Tags[-:-:-]\n"
		for k, val := range inst.Tags {
			text += fmt.Sprintf("  [darkgray]  %-24s[-] [white]%s[-]\n", k, val)
		}
	}

	if len(inst.SecurityGroups) > 0 {
		text += "\n  [yellow::b]Security Groups[-:-:-]\n"
		for _, sg := range inst.SecurityGroups {
			text += fmt.Sprintf("  [aqua]  %-22s[-] [darkgray]%s[-]\n", sg.ID, sg.Name)
		}
	}

	text += "\n  [darkgray][Esc] Back  [r] Refresh[-]"

	v.detail.SetText(text)
	v.detail.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			v.pages.SwitchToPage("list")
			v.app.SetFocus(v.table)
			return nil
		}
		switch event.Rune() {
		case 'r', 'R':
			v.pages.SwitchToPage("list")
			v.app.SetFocus(v.table)
		}
		return event
	})

	v.pages.AddAndSwitchToPage("detail", v.detail, true)
	v.app.SetFocus(v.detail)
}

func (v *EC2View) openFilter() {
	input := tview.NewInputField().
		SetLabel("  Filter (name/id): ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)

	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			v.filter = input.GetText()
		} else if key == tcell.KeyEscape {
			v.filter = ""
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.table)
		v.updateTable()
	})

	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.table, 0, 1, false).
		AddItem(input, 1, 0, true)

	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
}

func (v *EC2View) showTableError(msg string) {
	v.table.Clear()
	v.table.SetCell(1, 0, tview.NewTableCell("  [red]Error: "+msg+"[-]").
		SetTextColor(tcell.ColorRed).SetSelectable(false))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func orDash(s string) string {
	if s == "" {
		return "─"
	}
	return s
}

