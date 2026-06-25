package views

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
)

// SGView shows security groups with drill-down into inbound/outbound rules.
type SGView struct {
	app    *tview.Application
	client *aws.Client

	pages   *tview.Pages
	sgTable *tview.Table
	sgs     []aws.SecurityGroup
}

func NewSGView(app *tview.Application, client *aws.Client) *SGView {
	v := &SGView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.sgTable = newStyledTable(" Security Groups  <Enter> Rules ")
	v.sgTable.SetSelectedFunc(func(row, col int) {
		if row > 0 && row <= len(v.sgs) {
			v.openRules(v.sgs[row-1])
		}
	})
	v.pages.AddPage("list", v.sgTable, true, true)
	return v
}

func (v *SGView) GetContent() tview.Primitive  { return v.pages }
func (v *SGView) GetFocusable() tview.Primitive { return v.sgTable }

func (v *SGView) Refresh(ctx context.Context) {
	showLoading(v.app, v.sgTable)
	sgs, err := v.client.ListSecurityGroups(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.sgTable, err.Error()) })
		return
	}
	v.sgs = sgs
	v.app.QueueUpdateDraw(func() { v.updateSGTable() })
}

func (v *SGView) updateSGTable() {
	v.sgTable.Clear()
	for col, h := range []string{"NAME", "GROUP ID", "VPC", "DESCRIPTION", "INBOUND", "OUTBOUND"} {
		v.sgTable.SetCell(0, col, headerCell(h))
	}
	for i, sg := range v.sgs {
		row := i + 1
		v.sgTable.SetCell(row, 0, tview.NewTableCell(" "+sg.Name).SetTextColor(tcell.ColorWhite))
		v.sgTable.SetCell(row, 1, tview.NewTableCell(" "+sg.ID).SetTextColor(tcell.ColorAqua))
		v.sgTable.SetCell(row, 2, tview.NewTableCell(" "+orDash(sg.VpcID)).SetTextColor(tcell.ColorDarkGray))
		v.sgTable.SetCell(row, 3, tview.NewTableCell(" "+sg.Description).SetTextColor(tcell.ColorDarkGray).SetMaxWidth(40))
		v.sgTable.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("  %d rules", len(sg.InboundRules))).SetTextColor(tcell.ColorWhite))
		v.sgTable.SetCell(row, 5, tview.NewTableCell(fmt.Sprintf("  %d rules", len(sg.OutboundRules))).SetTextColor(tcell.ColorWhite))
	}
	if len(v.sgs) == 0 {
		v.sgTable.SetCell(1, 0, tview.NewTableCell("  No security groups found").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *SGView) openRules(sg aws.SecurityGroup) {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	tv.SetBorder(true).
		SetTitle(fmt.Sprintf(" Security Group: %s  <Esc> Back ", sg.Name))
	tv.SetInputCapture(escBack(v.app, v.pages, "list", v.sgTable))

	text := fmt.Sprintf("[aqua::b]  %s[-:-:-]  [darkgray]%s[-]\n\n", sg.Name, sg.ID)
	text += fmt.Sprintf("  [yellow]VPC[-]          [white]%s[-]\n", orDash(sg.VpcID))
	text += fmt.Sprintf("  [yellow]Description[-]  [darkgray]%s[-]\n\n", sg.Description)

	if len(sg.Tags) > 0 {
		text += "  [yellow::b]Tags[-:-:-]\n"
		for k, val := range sg.Tags {
			text += fmt.Sprintf("  [darkgray]  %-24s[-] [white]%s[-]\n", k, val)
		}
		text += "\n"
	}

	text += "  [yellow::b]Inbound Rules[-:-:-]\n"
	text += ruleTableHeader()
	if len(sg.InboundRules) == 0 {
		text += "  [darkgray]  (none)[-]\n"
	}
	for _, rule := range sg.InboundRules {
		text += ruleRow(rule)
	}

	text += "\n  [yellow::b]Outbound Rules[-:-:-]\n"
	text += ruleTableHeader()
	if len(sg.OutboundRules) == 0 {
		text += "  [darkgray]  (none)[-]\n"
	}
	for _, rule := range sg.OutboundRules {
		text += ruleRow(rule)
	}

	text += "\n  [darkgray][Esc] Back[-]"
	tv.SetText(text)

	v.pages.AddAndSwitchToPage("rules", tv, true)
	v.app.SetFocus(tv)
}

func ruleTableHeader() string {
	return fmt.Sprintf("  [yellow]%-8s  %-12s  %-30s  %s[-]\n",
		"PROTOCOL", "PORT RANGE", "SOURCE/DEST", "DESCRIPTION")
}

func ruleRow(rule aws.SGRule) string {
	src := orDash(rule.Source)
	desc := rule.Description
	if len(src) > 30 {
		src = src[:27] + "…"
	}
	line := fmt.Sprintf("  [white]%-8s[-]  [aqua]%-12s[-]  [darkgray]%-30s[-]  [darkgray]%s[-]\n",
		rule.Protocol, orDash(rule.PortRange), src, desc)
	return line
}
