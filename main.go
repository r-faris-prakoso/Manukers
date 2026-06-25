package main

import (
	"flag"
	"fmt"
	"os"

	"manukers/internal/aws"
	"manukers/internal/ui"
)

func main() {
	region := flag.String("region", "ap-northeast-1", "AWS region")
	profile := flag.String("profile", "", "AWS profile name (optional)")
	flag.Parse()

	client, err := aws.NewClient(*region, *profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing AWS client: %v\n", err)
		os.Exit(1)
	}

	app := ui.NewApp(client)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
