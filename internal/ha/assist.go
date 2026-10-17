package ha

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotAdmin is HA refusing a command to a token whose user is no
// administrator (websocket_api.require_admin): the Assist runs are such.
var ErrNotAdmin = errors.New("ha: the token's user is not an administrator")

// AssistRun is one Assist run as HA's pipeline debug store keeps it: the
// sentence and how HA's conversation answered it. Nothing else of the run
// (device, satellite, conversation) is kept.
type AssistRun struct {
	ID       string
	Time     time.Time
	Done     bool   // the run has ended (intent-end, run-end or error)
	Intent   bool   // it reached the conversation (intent-end)
	Sentence string // intent-start's intent_input
	Local    bool   // intent-end's processed_locally: HA's own intents answered
	Failed   bool   // the response was an error (no_intent_match, no_valid_targets, …)
}

// AssistRuns reads the runs HA keeps of every Assist pipeline (the last ten
// each, in memory; assist_pipeline/run.py STORED_PIPELINE_RUNS), those
// from since on whose id known does not know, over one WebSocket. The
// commands are admin-only: a refusal is ErrNotAdmin.
func (c *Client) AssistRuns(ctx context.Context, since time.Time, known func(id string) bool) ([]AssistRun, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	w, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer w.close()
	defer w.closeOn(ctx)()
	ask := func(cmd map[string]any, out any) error {
		id, err := w.send(cmd)
		if err != nil {
			return err
		}
		return w.result(id, out)
	}
	var list struct {
		Pipelines []struct {
			ID string `json:"id"`
		} `json:"pipelines"`
	}
	if err := ask(map[string]any{"type": "assist_pipeline/pipeline/list"}, &list); err != nil {
		return nil, err
	}
	var out []AssistRun
	for _, p := range list.Pipelines {
		var runs struct {
			Runs []struct {
				ID        string `json:"pipeline_run_id"`
				Timestamp string `json:"timestamp"`
			} `json:"pipeline_runs"`
		}
		if err := ask(map[string]any{"type": "assist_pipeline/pipeline_debug/list", "pipeline_id": p.ID}, &runs); err != nil {
			return nil, err
		}
		for _, r := range runs.Runs {
			at, err := time.Parse(time.RFC3339Nano, r.Timestamp)
			if err != nil || at.Before(since) || known(r.ID) {
				continue
			}
			var debug struct {
				Events []struct {
					Type string          `json:"type"`
					Data json.RawMessage `json:"data"`
				} `json:"events"`
			}
			if err := ask(map[string]any{"type": "assist_pipeline/pipeline_debug/get", "pipeline_id": p.ID, "pipeline_run_id": r.ID}, &debug); err != nil {
				return nil, err
			}
			run := AssistRun{ID: r.ID, Time: at}
			for _, e := range debug.Events {
				switch e.Type {
				case "intent-start":
					var d struct {
						Input string `json:"intent_input"`
					}
					json.Unmarshal(e.Data, &d)
					run.Sentence = d.Input
				case "intent-end":
					var d struct {
						Local  bool `json:"processed_locally"`
						Output struct {
							Response struct {
								Type string `json:"response_type"`
							} `json:"response"`
						} `json:"intent_output"`
					}
					json.Unmarshal(e.Data, &d)
					run.Done, run.Intent, run.Local, run.Failed = true, true, d.Local, d.Output.Response.Type == "error"
				case "run-end", "error":
					run.Done = true
				}
			}
			out = append(out, run)
		}
	}
	return out, nil
}
