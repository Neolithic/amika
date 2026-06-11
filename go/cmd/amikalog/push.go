package main

import (
	"fmt"
	"os"

	"github.com/gofixpoint/amika/go/internal/apiclient"
	"github.com/gofixpoint/amika/go/internal/config"
	"github.com/gofixpoint/amika/go/internal/eventlog"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "beta:push",
	Short: "Upload captured events to your organization",
	Long: `Upload captured events that have not been pushed yet. Repeated runs upload
only events captured since the last push.

Set AMIKA_API_KEY to authenticate.`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		key := os.Getenv(config.EnvAPIKey)
		if key == "" {
			return fmt.Errorf("set %s to push; amikalog authenticates with an org API key only", config.EnvAPIKey)
		}
		stateDir, err := config.StateDir()
		if err != nil {
			return fmt.Errorf("resolving state directory: %w", err)
		}

		client := apiclient.NewClientWithTokenSource(config.APIURL(), apiclient.NewStaticTokenSource(key))
		report, err := eventlog.Push(stateDir, apiUploader{client: client})
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "uploaded %d, skipped %d, failed %d\n", report.Uploaded, report.Skipped, report.Failed)
		if report.Failed > 0 {
			for _, e := range report.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "amikalog: %v\n", e)
			}
			return fmt.Errorf("%d file(s) failed to upload", report.Failed)
		}
		return nil
	},
}

// apiUploader adapts the Amika API client to eventlog.Uploader: it requests a
// signed URL for each object key and PUTs the bytes to it.
type apiUploader struct {
	client *apiclient.Client
}

func (a apiUploader) Upload(objectKey string, data []byte) error {
	resp, err := a.client.CreateUploadBatch(apiclient.CreateUploadBatchRequest{
		// Upsert so re-pushing is idempotent: event files are append-only and
		// their object keys are deterministic, so if a PUT succeeded but the
		// manifest write was lost (interrupted run, failed write), the next push
		// re-uploads identical bytes and must overwrite rather than fail on the
		// already-existing object.
		Files: []apiclient.UploadFile{{Filename: objectKey, Upsert: true}},
	})
	if err != nil {
		return err
	}
	if len(resp.Objects) == 0 {
		return fmt.Errorf("no signed upload URL returned for %s", objectKey)
	}
	return a.client.UploadToSignedURL(resp.Objects[0].UploadURL, data, "application/json")
}

func init() {
	rootCmd.AddCommand(pushCmd)
}
