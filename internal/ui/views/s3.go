package views

import (
	"context"
	"fmt"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"manukers/internal/aws"
)

// S3View shows S3 buckets with drill-down detail.
type S3View struct {
	app     *tview.Application
	client  *aws.Client

	pages    *tview.Pages
	s3Table  *tview.Table
	buckets  []aws.S3Bucket
}

func NewS3View(app *tview.Application, client *aws.Client) *S3View {
	v := &S3View{
		app:    app,
		client: client,
		pages:  tview.NewPages(),
	}
	v.s3Table = newStyledTable(" S3 Buckets  <Enter> Details ")
	v.s3Table.SetSelectedFunc(func(row, col int) {
		if row > 0 && row <= len(v.buckets) {
			v.openBucketDetail(v.buckets[row-1])
		}
	})
	v.pages.AddPage("list", v.s3Table, true, true)
	return v
}

func (v *S3View) GetContent() tview.Primitive  { return v.pages }
func (v *S3View) GetFocusable() tview.Primitive { return v.s3Table }

func (v *S3View) Refresh(ctx context.Context) {
	showLoading(v.app, v.s3Table)
	buckets, err := v.client.ListBuckets(ctx)
	if err != nil {
		v.app.QueueUpdateDraw(func() { showTableError(v.s3Table, err.Error()) })
		return
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Name < buckets[j].Name })
	v.buckets = buckets
	v.app.QueueUpdateDraw(func() { v.updateS3Table() })
}

func (v *S3View) updateS3Table() {
	v.s3Table.Clear()
	for col, h := range []string{"BUCKET NAME", "CREATED"} {
		v.s3Table.SetCell(0, col, headerCell(h))
	}
	for i, b := range v.buckets {
		row := i + 1
		created := "─"
		if !b.CreatedAt.IsZero() {
			created = b.CreatedAt.Format("2006-01-02 15:04")
		}
		v.s3Table.SetCell(row, 0, tview.NewTableCell(" "+b.Name).SetTextColor(tcell.ColorWhite))
		v.s3Table.SetCell(row, 1, tview.NewTableCell(" "+created).SetTextColor(tcell.ColorDarkGray))
	}
	if len(v.buckets) == 0 {
		v.s3Table.SetCell(1, 0, tview.NewTableCell("  No S3 buckets found").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
	}
}

func (v *S3View) openBucketDetail(bucket aws.S3Bucket) {
	ctx := context.Background()

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	tv.SetBorder(true).
		SetTitle(fmt.Sprintf(" Bucket: %s  <Esc> Back ", bucket.Name))
	tv.SetInputCapture(escBack(v.app, v.pages, "list", v.s3Table))

	tv.SetText("  [darkgray]Fetching bucket region…[-]")
	v.pages.AddAndSwitchToPage("detail", tv, true)
	v.app.SetFocus(tv)

	go func() {
		region, err := v.client.GetBucketRegion(ctx, bucket.Name)
		created := "─"
		if !bucket.CreatedAt.IsZero() {
			created = bucket.CreatedAt.Format("2006-01-02 15:04:05")
		}

		text := fmt.Sprintf("[aqua::b]  %s[-:-:-]\n\n", bucket.Name)
		text += fmt.Sprintf("  [yellow]Created  [-][white]%s[-]\n", created)
		if err == nil {
			text += fmt.Sprintf("  [yellow]Region   [-][white]%s[-]\n", region)
		}
		text += fmt.Sprintf("\n  [yellow]S3 URI   [-][aqua]s3://%s[-]\n", bucket.Name)
		text += fmt.Sprintf("  [yellow]ARN      [-][darkgray]arn:aws:s3:::%s[-]\n", bucket.Name)
		text += "\n  [darkgray]Note: Object listing is available via AWS CLI or Console.[-]\n"
		text += "  [darkgray]aws s3 ls s3://" + bucket.Name + "/ --recursive[-]\n"
		text += "\n  [darkgray][Esc] Back[-]"

		v.app.QueueUpdateDraw(func() {
			tv.SetText(text)
		})
	}()
}
