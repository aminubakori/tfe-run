package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/go-tfe"
	"github.com/kvrhdn/tfe-run/io"
)

func main() {
	err := run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var output io.Output
	defer io.WriteOutput(&output)

	input, err := io.ReadInput()
	if err != nil {
		return fmt.Errorf("could not read input: %w", err)
	}

	config := &tfe.Config{
		Token: input.Token,
	}
	client, err := tfe.NewClient(config)
	if err != nil {
		return fmt.Errorf("could not create a new TFE client: %w", err)
	}

	ctx := context.Background()

	w, err := client.Workspaces.Read(ctx, input.Organization, input.Workspace)
	if err != nil {
		return fmt.Errorf("could not retrieve workspace '%v/%v': %w", input.Organization, input.Workspace, err)
	}

	cvOptions := tfe.ConfigurationVersionCreateOptions{
		// Don't automatically queue the runs, we create the run manually to set the message
		AutoQueueRuns: pb(false),
		Speculative:   &input.Speculative,
	}
	cv, err := client.ConfigurationVersions.Create(ctx, w.ID, cvOptions)
	if err != nil {
		if err.Error() == "resource not found" {
			return fmt.Errorf("could not create configuration version (not found), this might happen if you are not using a user or team API token")
		}
		return fmt.Errorf("could not create a new configuration version: %w", err)
	}

	fmt.Print("Uploading directory...\n")

	err = client.ConfigurationVersions.Upload(ctx, cv.UploadURL, input.Directory)
	if err != nil {
		return fmt.Errorf("could not upload directory '%v': %w", input.Directory, err)
	}

	fmt.Print("Done uploading.\n")

	rOptions := tfe.RunCreateOptions{
		Workspace:            w,
		ConfigurationVersion: cv,
		Message:              &input.Message,
	}
	r, err := client.Runs.Create(ctx, rOptions)
	if err != nil {
		return fmt.Errorf("could not create run: %w", err)
	}

	runURL := fmt.Sprintf(
		"https://app.terraform.io/app/%v/workspaces/%v/runs/%v",
		input.Organization, input.Workspace, r.ID,
	)

	fmt.Printf("Run %v has been queued\n", r.ID)
	fmt.Printf("View the run online: %v\n", runURL)

	output.RunURL = runURL

	// If auto apply isn't enabled a run could hang for a long time, even if
	// the run itself wouldn't change anything the previous run could still be
	// blocked waiting for confirmation.
	// Speculative runs can always continue it seems.
	if !input.Speculative && !w.AutoApply {
		fmt.Print("Auto apply isn't enabled, won't wait for completion.\n")
		return nil
	}

	var prevStatus tfe.RunStatus
	for {
		r, err = client.Runs.Read(ctx, r.ID)
		if err != nil {
			return fmt.Errorf("could not read run '%v': %v", r.ID, err)
		}

		if prevStatus != r.Status {
			fmt.Printf("Run status: %v\n", r.Status)
			prevStatus = r.Status
		}

		switch r.Status {

		case tfe.RunPlannedAndFinished:
			output.HasChanges = r.HasChanges
			fmt.Println("Run has been planned, nothing to do.")
			return nil
		case tfe.RunApplied:
			output.HasChanges = r.HasChanges
			fmt.Println("Run has been applied!")

		case tfe.RunCanceled:
			return fmt.Errorf("run %v has been canceled", r.ID)
		case tfe.RunDiscarded:
			return fmt.Errorf("run %v has been discarded", r.ID)
		case tfe.RunErrored:
			return fmt.Errorf("run %v has errored", r.ID)
		}
	}
}

func pb(value bool) *bool {
	local := value
	return &local
}
