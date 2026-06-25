package views

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PickerItem describes one selectable resource in the launcher.
type PickerItem struct {
	Name   string
	Desc   string
	ViewID string
}

// All available resource types shown in the picker.
var PickerItems = []PickerItem{
	{"EC2", "Instances and security groups", "ec2"},
	{"Load Balancers", "ALB / NLB, listeners and routing rules", "lb"},
	{"Target Groups", "Target health and monitoring", "tg"},
	{"Security Groups", "Inbound and outbound rules", "sg"},
	{"EKS", "Clusters, node groups and add-ons", "eks"},
	{"ECR", "Container repositories and images", "ecr"},
	{"S3", "Buckets", "s3"},
	{"Connection Graph", "LB → Listeners → Target Groups → EC2", "graph"},
}

// compactArt is the eagle logo used on the picker screen.
const compactArt = "" +
	"                        z$b\n" +
	"               .e$$$b.  $$$F  .d$$be\n" +
	"           .d$$$$$$$$$$e$$$be$$$$$$$$$$e.\n" +
	"       .e$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$b.\n" +
	"     z$$$$$$$P**\"\"**$$$$$$$$$$$P*\"\"\"\"***$$$$$b.\n" +
	"   z$$$$*\"            \"$$$$$$\"            \"*$$$$c\n" +
	" z$$*\"                 ^$$$$                  \"*$$.\n" +
	"^\"                      $$$F                      ^%\n" +
	"                        $$$b\n" +
	"                        $P*$\n" +
	"                       4P  *r\n" +
	"                       4    %"

// PickerView is the entry screen — no AWS calls, just a resource selector.
type PickerView struct {
	app      *tview.Application
	root     *tview.Flex
	list     *tview.List
	onSelect func(viewID string)
}

func NewPickerView(app *tview.Application, region string, onSelect func(viewID string)) *PickerView {
	v := &PickerView{app: app, onSelect: onSelect}
	v.build(region)
	return v
}

func (v *PickerView) build(region string) {
	v.list = tview.NewList()
	v.list.SetHighlightFullLine(true)
	v.list.SetSelectedBackgroundColor(tcell.ColorNavy)
	v.list.SetSelectedTextColor(tcell.ColorWhite)
	v.list.ShowSecondaryText(true)
	v.list.SetSecondaryTextColor(tcell.ColorDarkGray)
	v.list.SetWrapAround(true)

	for _, item := range PickerItems {
		id := item.ViewID // capture
		v.list.AddItem(
			fmt.Sprintf("  %-22s", item.Name),
			fmt.Sprintf("    %s", item.Desc),
			0,
			func() { v.onSelect(id) },
		)
	}

	// Art is left-aligned inside a fixed 52-char view (width of the longest art
	// line) so its internal spacing is preserved. The FlexColumn spacers centre
	// that block within the panel without distorting individual lines.
	artView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	artView.SetText("[yellow]" + compactArt + "[-]")

	artRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(artView, 52, 0, false).
		AddItem(tview.NewBox(), 0, 1, false)

	titleView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	titleView.SetText(
		"[aqua::b]MANUKERS[-:-:-]\n" +
			"[darkgray]AWS Infrastructure Explorer[-]\n" +
			fmt.Sprintf("[white]region:[-] [aqua]%s[-]\n", region),
	)

	footer := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	footer.SetBackgroundColor(tcell.ColorDarkSlateGray)
	footer.SetText("[yellow]↑↓[-] Navigate  [yellow]Enter[-] Open  [yellow]q[-] Quit  [yellow]:[-] Command")

	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(artRow, 12, 0, false).  // 12 art lines, preserved layout
		AddItem(titleView, 5, 0, false). // MANUKERS + subtitle + region + spacing
		AddItem(v.list, 0, 1, true).
		AddItem(footer, 1, 0, false)
	panel.SetBorder(true).SetBorderColor(tcell.ColorDarkCyan)

	// Centre horizontally only; panel fills full terminal height so the list
	// always has enough room to be navigable.
	v.root = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false). // left spacer
		AddItem(panel, 68, 0, true).          // fixed 68-char wide, full height
		AddItem(tview.NewBox(), 0, 1, false)  // right spacer
}

func (v *PickerView) GetContent() tview.Primitive  { return v.root }
func (v *PickerView) GetFocusable() tview.Primitive { return v.list }

// Filter narrows the list to items whose name or desc contains substr.
func (v *PickerView) Filter(substr string) {
	v.list.Clear()
	for _, item := range PickerItems {
		if substr == "" || containsFold(item.Name, substr) || containsFold(item.Desc, substr) {
			id := item.ViewID
			v.list.AddItem(
				fmt.Sprintf("  %-22s", item.Name),
				fmt.Sprintf("    %s", item.Desc),
				0,
				func() { v.onSelect(id) },
			)
		}
	}
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	sl, subl := []rune(s), []rune(sub)
outer:
	for i := 0; i <= len(sl)-len(subl); i++ {
		for j, r := range subl {
			if toLower(sl[i+j]) != toLower(r) {
				continue outer
			}
		}
		return true
	}
	return false
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}
