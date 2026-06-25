package views

import (
	"context"
	"fmt"
	"sync"

	"github.com/rivo/tview"

	"manukers/internal/aws"
	"manukers/internal/theme"
)

// OverviewView shows a summary dashboard of all AWS resources.
type OverviewView struct {
	app    *tview.Application
	client *aws.Client
	text   *tview.TextView
}

func NewOverviewView(app *tview.Application, client *aws.Client) *OverviewView {
	v := &OverviewView{
		app:    app,
		client: client,
		text:   tview.NewTextView().SetDynamicColors(true).SetScrollable(true),
	}
	v.text.SetBorder(true).SetTitle(" Overview — AWS Infrastructure ")
	v.text.SetText("  [darkgray]Loading…[-]")
	return v
}

func (v *OverviewView) GetContent() tview.Primitive  { return v.text }
func (v *OverviewView) GetFocusable() tview.Primitive { return v.text }

func (v *OverviewView) Refresh(ctx context.Context) {
	var (
		instances []aws.Instance
		lbs       []aws.LoadBalancer
		tgs       []aws.TargetGroup
		sgs       []aws.SecurityGroup
		clusters  []aws.EKSCluster
		wg        sync.WaitGroup
		mu        sync.Mutex
		errs      []string
	)

	do := func(fn func() error) {
		defer wg.Done()
		if err := fn(); err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
		}
	}

	wg.Add(5)
	go do(func() error { var e error; instances, e = v.client.ListInstances(ctx); return e })
	go do(func() error { var e error; lbs, e = v.client.ListLoadBalancers(ctx); return e })
	go do(func() error { var e error; tgs, e = v.client.ListTargetGroups(ctx); return e })
	go do(func() error { var e error; sgs, e = v.client.ListSecurityGroups(ctx); return e })
	go do(func() error { var e error; clusters, e = v.client.ListClusters(ctx); return e })
	wg.Wait()

	// Aggregate counts
	runningEC2 := 0
	for _, i := range instances {
		if i.State == "running" {
			runningEC2++
		}
	}
	activeLBs := 0
	for _, lb := range lbs {
		if lb.State == "active" {
			activeLBs++
		}
	}
	activeEKS := 0
	for _, c := range clusters {
		if c.Status == "ACTIVE" {
			activeEKS++
		}
	}

	w := 48
	sep := fmt.Sprintf("  [yellow]%s[-]\n", pad("─", w))

	out := "\n"
	out += "  [aqua::b]╔══════════════════════════════════════════════╗[-:-:-]\n"
	out += "  [aqua::b]║         AWS Infrastructure Overview          ║[-:-:-]\n"
	out += "  [aqua::b]╚══════════════════════════════════════════════╝[-:-:-]\n\n"

	box := func(title string, lines ...string) string {
		s := fmt.Sprintf("  [yellow]┌─ %-*s─┐[-]\n", w-4, title+" ")
		for _, l := range lines {
			s += fmt.Sprintf("  [yellow]│[-]  %-*s[yellow]│[-]\n", w-4, l)
		}
		s += fmt.Sprintf("  [yellow]└%s┘[-]\n\n", pad("─", w-2))
		return s
	}

	ec2Color := "green"
	if runningEC2 == 0 && len(instances) > 0 {
		ec2Color = "yellow"
	}
	out += box("EC2 Instances",
		fmt.Sprintf("Total: [white]%d[-]", len(instances)),
		fmt.Sprintf("Running: [%s]%s %d[-]", ec2Color, theme.StateIcon("running"), runningEC2),
		healthBar("", runningEC2, len(instances)),
	)

	lbColor := "green"
	if activeLBs == 0 && len(lbs) > 0 {
		lbColor = "yellow"
	}
	out += box("Load Balancers",
		fmt.Sprintf("Total: [white]%d[-]", len(lbs)),
		fmt.Sprintf("Active: [%s]%s %d[-]", lbColor, theme.StateIcon("active"), activeLBs),
	)

	out += box("Target Groups",
		fmt.Sprintf("Total: [white]%d[-]", len(tgs)),
	)

	out += box("Security Groups",
		fmt.Sprintf("Total: [white]%d[-]", len(sgs)),
	)

	eksColor := "green"
	if activeEKS == 0 && len(clusters) > 0 {
		eksColor = "yellow"
	}
	out += box("EKS Clusters",
		fmt.Sprintf("Total: [white]%d[-]", len(clusters)),
		fmt.Sprintf("Active: [%s]%s %d[-]", eksColor, theme.StateIcon("ACTIVE"), activeEKS),
	)

	_ = sep // used implicitly above

	if len(errs) > 0 {
		out += "  [red]Errors:[-]\n"
		for _, e := range errs {
			out += fmt.Sprintf("  [red]• %s[-]\n", e)
		}
		out += "\n"
	}

	out += "  [darkgray]Press [1]–[7] to navigate  •  [r] Refresh[-]\n"

	v.app.QueueUpdateDraw(func() {
		v.text.SetText(out)
	})
}

func pad(ch string, n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += ch
	}
	return s
}

func healthBar(label string, healthy, total int) string {
	_ = label
	if total == 0 {
		return "[darkgray]No data[-]"
	}
	const width = 20
	filled := (healthy * width) / total
	bar := "["
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	bar += fmt.Sprintf("] [white]%d/%d[-]", healthy, total)
	return bar
}
