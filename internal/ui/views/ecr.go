package views

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
)

// ECRView shows ECR repositories with drill-down into images.
type ECRView struct {
	app       *tview.Application
	client    *aws.Client
	pages     *tview.Pages
	repoTable *tview.Table
	repos     []aws.ECRRepository
	filter    string
}

func NewECRView(app *tview.Application, client *aws.Client) *ECRView {
	v := &ECRView{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.repoTable = newStyledTable(" ECR Repositories  <Enter> Images  </> Filter ")
	v.repoTable.SetSelectedFunc(func(row, col int) {
		cell := v.repoTable.GetCell(row, 0)
		if cell == nil || cell.GetReference() == nil {
			return
		}
		v.openImages(*cell.GetReference().(*aws.ECRRepository))
	})
	v.repoTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '/' {
			v.openFilter()
			return nil
		}
		return event
	})
	v.pages.AddPage("list", v.repoTable, true, true)
	return v
}

func (v *ECRView) GetContent() tview.Primitive  { return v.pages }
func (v *ECRView) GetFocusable() tview.Primitive { return v.repoTable }

func (v *ECRView) Refresh(ctx context.Context) {
	showLoading(v.app, v.repoTable)
	repos, err := v.client.ListRepositories(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.repoTable, err.Error()) })
		return
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	v.repos = repos
	v.app.QueueUpdateDraw(func() { v.updateRepoTable() })
}

func (v *ECRView) updateRepoTable() {
	v.repoTable.Clear()
	if v.filter != "" {
		v.repoTable.SetTitle(fmt.Sprintf(" ECR Repositories  /%s  </> Filter ", v.filter))
	} else {
		v.repoTable.SetTitle(" ECR Repositories  <Enter> Images  </> Filter ")
	}
	for col, h := range []string{"NAME", "URI", "MUTABILITY", "SCAN ON PUSH", "CREATED"} {
		v.repoTable.SetCell(0, col, headerCell(h))
	}
	row := 1
	for i := range v.repos {
		repo := v.repos[i]
		if v.filter != "" && !strings.Contains(strings.ToLower(repo.Name), strings.ToLower(v.filter)) {
			continue
		}
		mut := repo.Mutability
		mutColor := tcell.ColorGreen
		if mut == "IMMUTABLE" {
			mutColor = tcell.ColorAqua
		}
		created := "─"
		if !repo.CreatedAt.IsZero() {
			created = repo.CreatedAt.Format("2006-01-02")
		}
		scanText := "off"
		scanColor := tcell.ColorRed
		if repo.ScanOnPush {
			scanText = "on"
			scanColor = tcell.ColorGreen
		}
		v.repoTable.SetCell(row, 0, tview.NewTableCell(" "+repo.Name).
			SetTextColor(tcell.ColorWhite).SetReference(&v.repos[i]))
		v.repoTable.SetCell(row, 1, tview.NewTableCell(" "+repo.URI).SetTextColor(tcell.ColorDarkGray).SetMaxWidth(60))
		v.repoTable.SetCell(row, 2, tview.NewTableCell(" "+mut).SetTextColor(mutColor))
		v.repoTable.SetCell(row, 3, tview.NewTableCell(" "+scanText).SetTextColor(scanColor))
		v.repoTable.SetCell(row, 4, tview.NewTableCell(" "+created).SetTextColor(tcell.ColorDarkGray))
		row++
	}
	if row == 1 {
		msg := "  No ECR repositories found"
		if v.filter != "" {
			msg = fmt.Sprintf("  No results for \"%s\"  [Esc to clear]", v.filter)
		}
		v.repoTable.SetCell(1, 0, tview.NewTableCell(msg).SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *ECRView) openFilter() {
	prev := v.filter
	input := tview.NewInputField().
		SetLabel("  / Filter: ").
		SetFieldWidth(30).
		SetText(v.filter).
		SetFieldTextColor(tcell.ColorWhite).
		SetFieldBackgroundColor(tcell.ColorDarkSlateBlue)
	input.SetChangedFunc(func(text string) {
		v.filter = text
		v.updateRepoTable()
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			v.filter = prev // Esc reverts to whatever was active before
		}
		v.pages.RemovePage("filter")
		v.app.SetFocus(v.repoTable)
		v.updateRepoTable()
	})
	filterLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.repoTable, 0, 1, false).
		AddItem(input, 1, 0, true)
	v.pages.AddAndSwitchToPage("filter", filterLayout, true)
	v.app.SetFocus(input)
}

// openImages shows a loading page instantly, then fetches images in the background.
func (v *ECRView) openImages(repo aws.ECRRepository) {
	// Step 1: show loading page — no API call, instant.
	tv := loadingText(fmt.Sprintf(" Images: %s ", repo.Name))
	tv.SetInputCapture(escBack(v.app, v.pages, "list", v.repoTable))
	v.pages.AddAndSwitchToPage("images", tv, true)
	v.app.SetFocus(tv)

	// Step 2: fetch in background.
	go func() {
		ctx := context.Background()
		images, err := v.client.ListImages(ctx, repo.Name)
		if err != nil {
			v.app.QueueUpdateDraw(func() {
				tv.SetText(fmt.Sprintf("  [red]Error: %s[-]", err))
			})
			return
		}
		sort.Slice(images, func(i, j int) bool {
			return images[i].PushedAt.After(images[j].PushedAt)
		})
		text := renderImages(repo, images)
		v.app.QueueUpdateDraw(func() {
			tv.SetTitle(fmt.Sprintf(" Images: %s  <Esc> Back ", repo.Name))
			tv.SetText(text)
		})
	}()
}

func renderImages(repo aws.ECRRepository, images []aws.ECRImage) string {
	text := fmt.Sprintf("[aqua::b]  %s[-:-:-]\n", repo.Name)
	text += fmt.Sprintf("  [darkgray]URI: %s[-]\n\n", repo.URI)
	text += fmt.Sprintf("  [yellow]Mutability  [-][white]%s[-]    [yellow]Scan on Push  [-][white]%v[-]\n\n",
		repo.Mutability, repo.ScanOnPush)
	text += fmt.Sprintf("  [yellow::b]Images (%d)[-:-:-]\n\n", len(images))

	if len(images) == 0 {
		text += "  [darkgray]No images found[-]\n"
	}
	for _, img := range images {
		tags := "  [darkgray](untagged)[-]"
		if len(img.Tags) > 0 {
			tags = fmt.Sprintf("  [green]%s[-]", strings.Join(img.Tags, ", "))
		}
		pushed := "─"
		if !img.PushedAt.IsZero() {
			pushed = img.PushedAt.Format("2006-01-02 15:04")
		}
		size := formatBytes(img.SizeBytes)
		scan := ""
		if img.ScanStatus != "" {
			scan = fmt.Sprintf("  [darkgray]scan: %s[-]", img.ScanStatus)
		}
		digest := img.Digest
		if len(digest) > 19 {
			digest = digest[:19] + "…"
		}
		text += fmt.Sprintf("  %s\n", tags)
		text += fmt.Sprintf("    [darkgray]digest:[-] [white]%s[-]  [darkgray]size:[-] [white]%s[-]  [darkgray]pushed:[-] [white]%s[-]%s\n\n",
			digest, size, pushed, scan)
	}
	text += "  [darkgray][Esc] Back[-]"
	return text
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
