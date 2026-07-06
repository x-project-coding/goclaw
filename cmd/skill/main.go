// Command skill is the static CLI an AI Employee's bash uses to call a 42bucks
// skill-service endpoint, without hand-writing curl or python.
//
// It is the code-context twin of the native call_skill_service tool: the tool
// runs server-side during a chat turn (minting the workspace token from the run
// context), while this binary runs inside a skill's exec — including the
// curl-blocked code-job sandbox where a shell script (the old `xskill`) dies —
// and reads the token + identity from the env vars the runtime injects into every
// exec (SKILL_RUNTIME_TOKEN, GOCLAW_WORKSPACE_ID, GOCLAW_USER_ID, GOCLAW_AGENT_ID).
//
// Both share one operation catalog (internal/skillcatalog) so a route that does
// not exist cannot be named, and a `raw` escape hatch keeps the ~60 untyped
// passthrough endpoints (and jobs/crm/drive via --base/--auth) reachable.
//
// Usage:
//
//	skill ls                                 list the typed operations
//	skill call <operation> [json]            call a typed operation (json arg or stdin)
//	skill raw <METHOD> <path> [flags]        call any endpoint (body on stdin)
//
// Exit codes: 0 = 2xx, 1 = HTTP error (body printed), 2 = usage error, 3 = missing
// runtime token. Weak-model friendly: the upstream {code,message} body is always
// printed to stdout and a "[skill] HTTP <code>" line to stderr.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
)

const maxResponseBytes = 8 << 20 // 8 MiB, mirrors the native tool's cap.

var httpClient = &http.Client{Timeout: 60 * time.Second}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch args[0] {
	case "ls", "list", "operations":
		fmt.Print(skillcatalog.Description())
	case "call":
		os.Exit(runCall(args[1:]))
	case "raw":
		os.Exit(runRaw(args[1:]))
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "skill: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `skill — call a 42bucks skill-service endpoint (auth + identity handled from env).

Commands:
  skill ls                          list the typed operations
  skill call <operation> [json]     call a typed operation; input as a JSON arg or on stdin
  skill raw <METHOD> <path> [flags] call any endpoint directly; JSON body on stdin

  raw flags:
    --base URL     override base (default $X_API_BASE_URL; e.g. https://jobs.42bucks.com)
    --auth NAME    override the auth header name (default Authorization: Bearer)
    --skill SLUG   override X-Skill-Slug (default: first path segment)
    -H 'H: v'      add a header (repeatable)

  path forms (call uses the catalog path; raw resolves like this):
    manage-view/set     -> $BASE/api/skill-services/manage-view/set
    /api/agent/x        -> $BASE/api/agent/x
    https://host/x      -> used verbatim

Examples:
  skill call manage-skills.catalog
  echo '{"sessionKey":"...","hints":{}}' | skill call manage-view.set
  skill call manage-qa.run-status '{"id":"run_123"}'
  echo '{"query":"..."}' | skill raw POST research/search
`)
}

// runCall executes a typed catalog operation. Returns the process exit code.
func runCall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "skill call: missing <operation>. Run `skill ls` for the list.")
		return 2
	}
	opID := args[0]
	op, ok := skillcatalog.Lookup(opID)
	if !ok {
		fmt.Fprintf(os.Stderr, "skill call: unknown operation %q. Valid operations:\n%s", opID, skillcatalog.Description())
		return 2
	}

	// Input JSON: positional arg wins, else stdin (if piped), else empty.
	raw := ""
	if len(args) > 1 {
		raw = strings.TrimSpace(args[1])
	} else if piped := readStdin(); piped != "" {
		raw = piped
	}
	input := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			fmt.Fprintf(os.Stderr, "skill call: input is not valid JSON object: %v\n", err)
			return 2
		}
	}

	// Fill {placeholders} in the path from input, removing them from the body.
	path := op.Path
	for _, name := range op.PathParams {
		val, ok := input[name].(string)
		if !ok || val == "" {
			fmt.Fprintf(os.Stderr, "skill call: operation %q requires a string input.%s\n", op.ID, name)
			return 2
		}
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(val))
		delete(input, name)
	}

	fullURL := skillcatalog.BaseURL() + "/api/skill-services" + path
	var body []byte
	switch op.Method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		if len(input) > 0 {
			q := url.Values{}
			for k, v := range input {
				q.Set(k, fmt.Sprintf("%v", v))
			}
			fullURL += "?" + q.Encode()
		}
	default:
		b, err := json.Marshal(input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skill call: could not encode input as JSON: %v\n", err)
			return 2
		}
		body = b
	}

	return do(op.Method, fullURL, op.Skill, "Authorization", body, nil, op.ID)
}

// runRaw executes an arbitrary request against a resolved path. Returns the exit code.
func runRaw(args []string) int {
	method, path := "", ""
	base := strings.TrimRight(os.Getenv("X_API_BASE_URL"), "/")
	authHdr := "Authorization"
	skill := ""
	var extra []string

	i := 0
	for i < len(args) {
		a := args[i]
		switch a {
		case "--base":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "skill raw: --base needs a value")
				return 2
			}
			base = strings.TrimRight(args[i+1], "/")
			i += 2
		case "--auth":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "skill raw: --auth needs a value")
				return 2
			}
			authHdr = args[i+1]
			i += 2
		case "--skill":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "skill raw: --skill needs a value")
				return 2
			}
			skill = args[i+1]
			i += 2
		case "-H":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "skill raw: -H needs a value")
				return 2
			}
			extra = append(extra, args[i+1])
			i += 2
		default:
			switch {
			case strings.HasPrefix(a, "-"):
				fmt.Fprintf(os.Stderr, "skill raw: unknown flag %q\n", a)
				return 2
			case method == "":
				method = strings.ToUpper(a)
			case path == "":
				path = a
			default:
				fmt.Fprintf(os.Stderr, "skill raw: unexpected argument %q\n", a)
				return 2
			}
			i++
		}
	}
	if method == "" || path == "" {
		fmt.Fprintln(os.Stderr, "usage: skill raw <METHOD> <path> [--base URL] [--auth NAME] [--skill SLUG] [-H 'H: v']...")
		return 2
	}

	// Resolve the path like xskill: verbatim URL / absolute path / bare skill-services path.
	var fullURL string
	switch {
	case strings.HasPrefix(path, "http://"), strings.HasPrefix(path, "https://"):
		fullURL = path
	case strings.HasPrefix(path, "/"):
		if base == "" {
			fmt.Fprintln(os.Stderr, "skill raw: no base URL ($X_API_BASE_URL unset and no --base)")
			return 2
		}
		fullURL = base + path
	default:
		if base == "" {
			base = skillcatalog.BaseURL()
		}
		fullURL = base + "/api/skill-services/" + path
		if skill == "" {
			skill = strings.SplitN(path, "/", 2)[0] // default slug = first segment
		}
	}

	var body []byte
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		// no body
	default:
		if piped := readStdinBytes(); len(piped) > 0 {
			body = piped
		}
	}

	return do(method, fullURL, skill, authHdr, body, extra, method+" "+path)
}

// do performs the request with the injected auth + identity headers and prints
// the response body + a status line. Returns 0 on 2xx, 1 on HTTP error, 3 if the
// runtime token is missing.
func do(method, fullURL, skill, authHdr string, body []byte, extra []string, label string) int {
	tok := os.Getenv("SKILL_RUNTIME_TOKEN")
	if tok == "" {
		fmt.Fprintln(os.Stderr, "skill: SKILL_RUNTIME_TOKEN missing from environment (the runtime injects it per turn)")
		return 3
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, fullURL, reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill: could not build request: %v\n", err)
		return 2
	}

	if authHdr == "Authorization" {
		req.Header.Set("Authorization", "Bearer "+tok)
	} else {
		req.Header.Set(authHdr, tok)
	}
	if ws := os.Getenv("GOCLAW_WORKSPACE_ID"); ws != "" {
		req.Header.Set("X-Workspace-Id", ws)
	}
	if skill != "" {
		req.Header.Set("X-Skill-Slug", skill)
	}
	if ag := os.Getenv("GOCLAW_AGENT_ID"); ag != "" {
		req.Header.Set("X-Agent-Id", ag)
	}
	if uid := os.Getenv("GOCLAW_USER_ID"); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range extra {
		if name, val, ok := strings.Cut(h, ":"); ok {
			req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(val))
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill: request to %s failed: %v\n", label, err)
		return 1
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	trimmed := strings.TrimSpace(string(respBody))
	if trimmed != "" {
		fmt.Println(trimmed)
	}
	fmt.Fprintf(os.Stderr, "[skill] HTTP %d %s\n", resp.StatusCode, label)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0
	}
	return 1
}

// readStdin returns trimmed piped stdin, or "" if stdin is a terminal/empty.
func readStdin() string { return strings.TrimSpace(string(readStdinBytes())) }

func readStdinBytes() []byte {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) != 0 {
		return nil // interactive terminal, nothing piped
	}
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, maxResponseBytes))
	return b
}
