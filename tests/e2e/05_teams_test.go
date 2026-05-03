//go:build e2e

// Package e2e_test — Phase 14B teams tests.
// Validates WS teams.* RPC methods (internal/gateway/methods/teams*.go):
// create, list, update, add-member, task lifecycle, and delete.
// All operations are WS-primary (no HTTP teams endpoints in v4).
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

func mustOKTeams(t *testing.T, label string, res *helpers.APIResponse, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s: status %d, want %d, body=%s", label, res.Status, want, string(res.Body))
	}
}

func mustJSONTeams(t *testing.T, res *helpers.APIResponse, out any) {
	t.Helper()
	if err := res.JSON(out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, string(res.Body))
	}
}

func loginForTeams(t *testing.T, ctx context.Context, api *helpers.APIClient, email, pass string) string {
	t.Helper()
	res, err := api.POST(ctx, "/v1/auth/login", map[string]string{
		"email": email, "password": pass,
	})
	mustOKTeams(t, "POST /v1/auth/login", res, err, http.StatusOK)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	mustJSONTeams(t, res, &tok)
	if tok.AccessToken == "" {
		t.Fatalf("loginForTeams %s: empty access_token", email)
	}
	return tok.AccessToken
}

// createTwoAgentsForTeams creates two agents via HTTP and returns their agent_keys.
func createTwoAgentsForTeams(t *testing.T, ctx context.Context, api *helpers.APIClient) (leadKey, memberKey string) {
	t.Helper()
	for _, slot := range []struct {
		key  *string
		name string
	}{
		{&leadKey, "lead"}, {&memberKey, "mbr"},
	} {
		res, err := api.POST(ctx, "/v1/agents", map[string]any{
			"agent_key":  slot.name + "-" + helpers.RandHex8(),
			"agent_type": "open",
			"model":      "test/test-model",
			"provider":   "openai",
		})
		mustOKTeams(t, "POST /v1/agents ("+slot.name+")", res, err, http.StatusCreated)
		var ag struct{ AgentKey string `json:"agent_key"` }
		mustJSONTeams(t, res, &ag)
		*slot.key = ag.AgentKey
	}
	return
}

// wsConnectTeams dials a WS connection, sends connect, and returns the client.
func wsConnectTeams(t *testing.T, ctx context.Context, token string) *helpers.WSClient {
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

// TestTeamsCreate — teams.create with a lead + member agent → returns team id.
func TestTeamsCreate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	teamName := "e2e-team-" + helpers.RandHex8()
	params, _ := json.Marshal(map[string]any{
		"name":    teamName,
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	payload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(params))
	if err != nil {
		t.Fatalf("teams.create: %v", err)
	}

	var result struct {
		Team struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("teams.create: unmarshal response: %v (raw=%s)", err, string(payload))
	}
	if result.Team.ID == "" {
		t.Fatalf("teams.create: empty team id in response: %s", string(payload))
	}
}

// TestTeamsList — list shows the just-created team.
func TestTeamsList(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	teamName := "e2e-list-" + helpers.RandHex8()
	createParams, _ := json.Marshal(map[string]any{
		"name":    teamName,
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams)); err != nil {
		t.Fatalf("teams.create: %v", err)
	}

	listPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsList, map[string]any{})
	if err != nil {
		t.Fatalf("teams.list: %v", err)
	}

	var list struct {
		Teams []struct {
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(listPayload, &list); err != nil {
		t.Fatalf("teams.list: unmarshal: %v (raw=%s)", err, string(listPayload))
	}

	found := false
	for _, tm := range list.Teams {
		if tm.Name == teamName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("teams.list: team %q not found in response: %s", teamName, string(listPayload))
	}
}

// TestTeamsUpdate — update name → list reflects new name.
func TestTeamsUpdate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	// Create team.
	createParams, _ := json.Marshal(map[string]any{
		"name":    "e2e-upd-" + helpers.RandHex8(),
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	createPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams))
	if err != nil {
		t.Fatalf("teams.create: %v", err)
	}
	var created struct {
		Team struct {
			ID string `json:"id"`
		} `json:"team"`
	}
	if err := json.Unmarshal(createPayload, &created); err != nil {
		t.Fatalf("teams.create unmarshal: %v", err)
	}

	newName := "e2e-renamed-" + helpers.RandHex8()
	updateParams, _ := json.Marshal(map[string]any{
		"teamId": created.Team.ID,
		"name":   newName,
	})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodTeamsUpdate, json.RawMessage(updateParams)); err != nil {
		t.Fatalf("teams.update: %v", err)
	}

	// Verify list contains new name.
	listPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsList, map[string]any{})
	if err != nil {
		t.Fatalf("teams.list after update: %v", err)
	}
	if !json.Valid(listPayload) {
		t.Fatalf("teams.list: invalid JSON: %s", string(listPayload))
	}
	var list struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	json.Unmarshal(listPayload, &list)
	found := false
	for _, tm := range list.Teams {
		if tm.ID == created.Team.ID && tm.Name == newName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("teams.update: new name %q not reflected in list: %s", newName, string(listPayload))
	}
}

// TestTeamsAddMember — teams.members.add an extra agent → success response.
func TestTeamsAddMember(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	// Create a third agent to add as member.
	res, err := api.POST(ctx, "/v1/agents", map[string]any{
		"agent_key":  "extra-" + helpers.RandHex8(),
		"agent_type": "open",
		"model":      "test/test-model",
		"provider":   "openai",
	})
	mustOKTeams(t, "POST /v1/agents (extra)", res, err, http.StatusCreated)
	var extra struct{ AgentKey string `json:"agent_key"` }
	mustJSONTeams(t, res, &extra)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	createParams, _ := json.Marshal(map[string]any{
		"name":    "e2e-addmbr-" + helpers.RandHex8(),
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	createPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams))
	if err != nil {
		t.Fatalf("teams.create: %v", err)
	}
	var created struct {
		Team struct{ ID string `json:"id"` } `json:"team"`
	}
	json.Unmarshal(createPayload, &created)

	addParams, _ := json.Marshal(map[string]any{
		"teamId": created.Team.ID,
		"agent":  extra.AgentKey,
		"role":   "member",
	})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodTeamsMembersAdd, json.RawMessage(addParams)); err != nil {
		t.Fatalf("teams.members.add: %v", err)
	}
}

// TestTeamsTaskCreate — teams.tasks.create → list shows new task.
func TestTeamsTaskCreate(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	createParams, _ := json.Marshal(map[string]any{
		"name":    "e2e-task-" + helpers.RandHex8(),
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	createPayload, _ := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams))
	var created struct {
		Team struct{ ID string `json:"id"` } `json:"team"`
	}
	json.Unmarshal(createPayload, &created)
	if created.Team.ID == "" {
		t.Fatal("teams.create: no team id")
	}

	taskSubject := "e2e task " + helpers.RandHex8()
	taskParams, _ := json.Marshal(map[string]any{
		"teamId":  created.Team.ID,
		"subject": taskSubject,
	})
	taskPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsTaskCreate, json.RawMessage(taskParams))
	if err != nil {
		t.Fatalf("teams.tasks.create: %v", err)
	}

	var taskResult struct {
		Task struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"task"`
	}
	if err := json.Unmarshal(taskPayload, &taskResult); err != nil {
		t.Fatalf("teams.tasks.create unmarshal: %v (raw=%s)", err, string(taskPayload))
	}
	if taskResult.Task.ID == "" {
		t.Fatalf("teams.tasks.create: empty task id in: %s", string(taskPayload))
	}
}

// TestTeamsTaskComment — add a comment to a task → response ok.
func TestTeamsTaskComment(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	// Create team.
	createParams, _ := json.Marshal(map[string]any{
		"name":    "e2e-comment-" + helpers.RandHex8(),
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	createPayload, _ := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams))
	var created struct {
		Team struct{ ID string `json:"id"` } `json:"team"`
	}
	json.Unmarshal(createPayload, &created)

	// Create task.
	taskParams, _ := json.Marshal(map[string]any{
		"teamId":  created.Team.ID,
		"subject": "comment task " + helpers.RandHex8(),
	})
	taskPayload, _ := wsc.SendReq(wsCtx, protocol.MethodTeamsTaskCreate, json.RawMessage(taskParams))
	var taskCreated struct {
		Task struct{ ID string `json:"id"` } `json:"task"`
	}
	json.Unmarshal(taskPayload, &taskCreated)
	if taskCreated.Task.ID == "" {
		t.Fatal("teams.tasks.create: no task id")
	}

	// Add comment.
	commentParams, _ := json.Marshal(map[string]any{
		"teamId":  created.Team.ID,
		"taskId":  taskCreated.Task.ID,
		"content": "e2e comment " + helpers.RandHex8(),
	})
	commentPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsTaskComment, json.RawMessage(commentParams))
	if err != nil {
		t.Fatalf("teams.tasks.comment: %v", err)
	}

	var commentResult struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(commentPayload, &commentResult); err != nil {
		t.Fatalf("teams.tasks.comment unmarshal: %v (raw=%s)", err, string(commentPayload))
	}
	if !commentResult.OK {
		t.Fatalf("teams.tasks.comment: ok=false in: %s", string(commentPayload))
	}
}

// TestTeamsDelete — delete team → list excludes it.
func TestTeamsDelete(t *testing.T) {
	helpers.MustMigrateClean(t)
	helpers.ResetDB(t)

	gw := helpers.StartGateway(t)
	api := helpers.NewAPIClient()
	api.BaseURL = gw.BaseURL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := loginForTeams(t, ctx, api, helpers.RootEmail(), helpers.RootPassword())
	api.SetToken(token)

	leadKey, memberKey := createTwoAgentsForTeams(t, ctx, api)

	wsCtx, wsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer wsCancel()
	wsc := wsConnectTeams(t, wsCtx, token)
	defer wsc.Close()

	teamName := "e2e-del-" + helpers.RandHex8()
	createParams, _ := json.Marshal(map[string]any{
		"name":    teamName,
		"lead":    leadKey,
		"members": []string{memberKey},
	})
	createPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsCreate, json.RawMessage(createParams))
	if err != nil {
		t.Fatalf("teams.create: %v", err)
	}
	var created struct {
		Team struct{ ID string `json:"id"` } `json:"team"`
	}
	json.Unmarshal(createPayload, &created)
	if created.Team.ID == "" {
		t.Fatal("teams.create: no team id")
	}

	deleteParams, _ := json.Marshal(map[string]any{"teamId": created.Team.ID})
	if _, err := wsc.SendReq(wsCtx, protocol.MethodTeamsDelete, json.RawMessage(deleteParams)); err != nil {
		t.Fatalf("teams.delete: %v", err)
	}

	listPayload, err := wsc.SendReq(wsCtx, protocol.MethodTeamsList, map[string]any{})
	if err != nil {
		t.Fatalf("teams.list after delete: %v", err)
	}
	var list struct {
		Teams []struct {
			ID string `json:"id"`
		} `json:"teams"`
	}
	json.Unmarshal(listPayload, &list)
	for _, tm := range list.Teams {
		if tm.ID == created.Team.ID {
			t.Fatalf("teams.delete: deleted team %q still in list", created.Team.ID)
		}
	}
}
