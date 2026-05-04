//go:build e2e

// Package e2e_test exercises WS cron.* RPC methods
// (internal/gateway/methods/cron.go): create with at/every/cron schedule
// kinds, list, and runs history shape. All operations are WS-primary (no
// HTTP cron endpoints in v4).
package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

func mustOKCron(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONCron(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

func loginForCron(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKCron(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONCron(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForCron %s: empty access_token", email)
	}
	return tok.AccessToken
}

func wsConnectCron(t *testing.T, ctx context.Context, token string) *helpers.WSClient {
	t.Helper()
	wsc, err := helpers.NewWSClient(ctx, token)
	if err != nil {
		t.Skipf("WS dial failed (gateway may not be up): %v", err)
	}
	if _, err := wsc.Connect(ctx, map[string]any{"locale": "en"}); err != nil {
		wsc.Close()
		t.Skipf("WS connect failed: %v", err)
	}
	return wsc
}

// cronName returns a valid slug for a cron job (lowercase alphanumeric + hyphens).
func cronName() string {
	return "e2e-cron-" + helpers.RandHex8()
}

// TestCronJobCreateAt — cron.create with kind:at + future timestamp → returns job id.
func TestCronJobCreateAt(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForCron(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectCron(t, wsCtx, token)
	defer wsc.Close()

	// Schedule 1 hour from now.
	atMs := time.Now().Add(time.Hour).UnixMilli()
	params, _ := json.Marshal(map[string]any{
		"name":    cronName(),
		"message": "e2e at-job test",
		"schedule": map[string]any{
			"kind": "at",
			"atMs": atMs,
		},
	})
	payload, err := wsc.SendReq(wsCtx, protocol.MethodCronCreate, json.RawMessage(params))
	if err != nil {
		t.Fatalf("cron.create (at): %v", err)
	}

	var result struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("cron.create (at): unmarshal: %v (raw=%s)", err, string(payload))
	}
	if result.Job.ID == "" {
		t.Fatalf("cron.create (at): empty job id in: %s", string(payload))
	}
}

// TestCronJobCreateEvery — cron.create with kind:every + everyMs interval → returns id.
func TestCronJobCreateEvery(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForCron(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectCron(t, wsCtx, token)
	defer wsc.Close()

	params, _ := json.Marshal(map[string]any{
		"name":    cronName(),
		"message": "e2e every-job test",
		"schedule": map[string]any{
			"kind":    "every",
			"everyMs": int64(3_600_000), // 1 hour in ms
		},
	})
	payload, err := wsc.SendReq(wsCtx, protocol.MethodCronCreate, json.RawMessage(params))
	if err != nil {
		t.Fatalf("cron.create (every): %v", err)
	}

	var result struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("cron.create (every): unmarshal: %v (raw=%s)", err, string(payload))
	}
	if result.Job.ID == "" {
		t.Fatalf("cron.create (every): empty job id in: %s", string(payload))
	}
}

// TestCronJobCreateExpr — cron.create with kind:cron + standard cron expression → returns id.
func TestCronJobCreateExpr(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForCron(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectCron(t, wsCtx, token)
	defer wsc.Close()

	params, _ := json.Marshal(map[string]any{
		"name":    cronName(),
		"message": "e2e cron-expr job test",
		"schedule": map[string]any{
			"kind": "cron",
			"expr": "*/5 * * * *",
		},
	})
	payload, err := wsc.SendReq(wsCtx, protocol.MethodCronCreate, json.RawMessage(params))
	if err != nil {
		t.Fatalf("cron.create (cron expr): %v", err)
	}

	var result struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("cron.create (cron expr): unmarshal: %v (raw=%s)", err, string(payload))
	}
	if result.Job.ID == "" {
		t.Fatalf("cron.create (cron expr): empty job id in: %s", string(payload))
	}
}

// TestCronJobList — cron.list returns at least one of the created jobs.
func TestCronJobList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForCron(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectCron(t, wsCtx, token)
	defer wsc.Close()

	// Create a job to guarantee list is non-empty.
	jobName := cronName()
	createParams, _ := json.Marshal(map[string]any{
		"name":    jobName,
		"message": "e2e list test",
		"schedule": map[string]any{
			"kind":    "every",
			"everyMs": int64(7_200_000),
		},
	})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodCronCreate, json.RawMessage(createParams)); err != nil {
		t.Fatalf("cron.create (for list): %v", err)
	}

	listPayload, err := wsc.SendReq(wsCtx, protocol.MethodCronList, map[string]any{"includeDisabled": true})
	if err != nil {
		t.Fatalf("cron.list: %v", err)
	}

	var listResult struct {
		Jobs []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(listPayload, &listResult); err != nil {
		t.Fatalf("cron.list unmarshal: %v (raw=%s)", err, string(listPayload))
	}
	if len(listResult.Jobs) == 0 {
		t.Fatalf("cron.list: empty jobs list after create (raw=%s)", string(listPayload))
	}

	found := false
	for _, j := range listResult.Jobs {
		if j.Name == jobName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cron.list: created job %q not found in list: %s", jobName, string(listPayload))
	}
}

// TestCronJobRunsHistory — cron.runs returns a valid list shape for a newly-created job.
// The runs slice may be empty for a job that has never fired — just assert wire shape.
func TestCronJobRunsHistory(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForCron(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectCron(t, wsCtx, token)
	defer wsc.Close()

	// Create a job to get a valid ID.
	createParams, _ := json.Marshal(map[string]any{
		"name":    cronName(),
		"message": "e2e runs-history test",
		"schedule": map[string]any{
			"kind":    "every",
			"everyMs": int64(3_600_000),
		},
	})
	createPayload, err := wsc.SendReq(wsCtx, protocol.MethodCronCreate, json.RawMessage(createParams))
	if err != nil {
		t.Fatalf("cron.create (for runs): %v", err)
	}
	var created struct {
		Job struct{ ID string `json:"id"` } `json:"job"`
	}
	if err := json.Unmarshal(createPayload, &created); err != nil {
		t.Fatalf("cron.create unmarshal: %v", err)
	}
	if created.Job.ID == "" {
		t.Fatal("cron.create: no job id")
	}

	runsParams, _ := json.Marshal(map[string]any{"jobId": created.Job.ID})
	runsPayload, err := wsc.SendReq(wsCtx, protocol.MethodCronRuns, json.RawMessage(runsParams))
	if err != nil {
		t.Fatalf("cron.runs: %v", err)
	}

	// Wire shape: must have a "runs" array (may be empty for a new job).
	var runsResult struct {
		Runs []json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(runsPayload, &runsResult); err != nil {
		t.Fatalf("cron.runs unmarshal: %v (raw=%s)", err, string(runsPayload))
	}
	// Runs field must be present (not nil). May be empty — that is valid for a new job.
	if runsResult.Runs == nil {
		t.Fatalf("cron.runs: missing runs key in response: %s", string(runsPayload))
	}
}
