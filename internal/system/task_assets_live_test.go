package system

import (
	"context"
	"os"
	"testing"
	"time"
)

// Explicit operator probe; regular tests never contact external sites.
func TestLivePublicPageReference(t *testing.T) {
	raw := os.Getenv("ORCHESTRATOR_REFERENCE_PROBE_URL")
	if raw == "" {
		t.Skip("set ORCHESTRATOR_REFERENCE_PROBE_URL to probe an authorized public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	j := &Job{Description: "[" + raw + "](" + raw + ")"}
	assets := downloadTaskAssets(ctx, taskReferenceURLs(j), assetHTTPClient())
	if len(assets) != 1 || assets[0].Error != "" || len(assets[0].Data) == 0 {
		for _, a := range assets {
			t.Logf("reference: %s error=%s bytes=%d", a.Source, a.Error, len(a.Data))
		}
		t.Fatal("public page was not downloaded exactly once")
	}
	a := assets[0]
	t.Logf("Downloaded %s: bytes=%d content-type=%s fetched=%s", a.Name, len(a.Data), a.ContentType, a.FetchedAt)
}
