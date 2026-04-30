package bot

import "fmt"

// Zalo Bot API error codes (HTTP-status-shaped) returned in the response
// envelope's `error_code` field. Source: docs/zalo-error-codes.md (bot-api
// section, scraped from https://bot.zapps.me/docs/error-code/).
//
// Note on code 403: the Zalo doc labels it "Internal server error", which is
// inconsistent with HTTP semantics but matches what the API actually returns.
// We stay faithful to the doc.
const (
	codeBotBadRequest          = 400
	codeBotUnauthorized        = 401
	codeBotInternalServerError = 403
	codeBotNotFound            = 404
	codeBotRequestTimeout      = 408
	codeBotQuotaExceeded       = 429
)

// botCodeHints maps a Zalo Bot error code to a one-sentence English hint
// that the LLM (or an operator reading the channel error) can act on.
// Unknown codes return the empty string and the legacy format is kept.
var botCodeHints = map[int]string{
	codeBotBadRequest:          "Zalo rejected the request as malformed; verify the bot endpoint path, method name, and required parameters.",
	codeBotUnauthorized:        "Zalo bot token is expired or invalid; the operator must regenerate the token before sends will resume.",
	codeBotInternalServerError: "Zalo returned an internal server error (Zalo labels code 403 this way); retry after a short backoff.",
	codeBotNotFound:            "Zalo could not find the target resource; verify chat_id / message_id / file_id before retrying.",
	codeBotRequestTimeout:      "Zalo took too long to process the request; retry after a short backoff.",
	codeBotQuotaExceeded:       "Zalo bot API rate limit exceeded; back off before retrying.",
}

// formatAPIError builds the user/agent-facing error string for a non-OK Zalo
// bot API response. When the code is in the catalog the hint is appended so
// the agent loop can self-correct without parsing the raw description.
func formatAPIError(code int, description string) error {
	if hint, ok := botCodeHints[code]; ok {
		return fmt.Errorf("zalo API error %d: %s — %s", code, description, hint)
	}
	return fmt.Errorf("zalo API error %d: %s", code, description)
}
