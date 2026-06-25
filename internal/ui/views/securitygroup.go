package views

import (
	"context"
	"fmt"
	"strings"

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
	filter  string
}

func NewSGView(app *tview.Application, client *aws.Client) *SGView {
	v := &SGView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.sgTable = newStyledTable(" Security Groups  <Enter> Rules  </> Filter ")
	v.sgTable.SetSelectedFunc(func(row, col int) {
		cell := v.sgTable.GetCell(row, 0)
		if cell == nil || cell.GetReference() == nil {
			return
		}
		v.openRules(*cell.GetReference().(*aws.SecurityGroup))
	})
	v.sgTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
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
	row := 1
	for i := range v.sgs {
		sg := v.sgs[i]
		if v.filter != "" &&
			!strings.Contains(strings.ToLower(sg.Name), strings.ToLower(v.filter)) &&
			!strings.Contains(strings.ToLower(sg.ID), strings.ToLower(v.filter)) {
			continue
		}
		v.sgTable.SetCell(row, 0, tview.NewTableCell(" "+sg.Name).
			SetTextColor(tcell.ColorWhite).SetReference(&v.sgs[i]))
		v.sgTable.SetCell(row, 1, tview.NewTableCell(" "+sg.ID).SetTextColor(tcell.ColorAqua))
		v.sgTable.SetCell(row, 2, tview.NewTableCell(" "+orDash(sg.VpcID)).SetTextColor(tcell.ColorDarkGray))
		v.sgTable.SetCell(row, 3, tview.NewTableCell(" "+sg.Description).SetTextColor(tcell.ColorDarkGray).SetMaxWidth(40))
		v.sgTable.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("  %d rules", len(sg.InboundRules))).SetTextColor(tcell.ColorWhite))
		v.sgTable.SetCell(row, 5, tview.NewTableCell(fmt.Sprintf("  %d rules", len(sg.OutboundRules))).SetTextColor(tcell.ColorWhite))
		row++
	}
	if row == 1 {
		msg := "  No security groups found"
		if v.filter != "" {
			msg = fmt.Sprintf("  No results for \"%s\"  [Esc to clear]", v.filter)
		}
		v.sgTable.SetCell(1, 0, tview.NewTableCell(msg).SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *SGView) openFilter() {
	input := tview.NewInputField().
		SetLabel("  / Filter: ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			v.filter = input.GetText()
		} else {
			v.filter = ""
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.sgTable)
		v.updateSGTable()
	})
	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.sgTable, 0, 1, false).
		AddItem(input, 1, 0, true)
	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
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
