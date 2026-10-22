package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

const bedrockProfileARN = "arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-3"

// noisyConverse is noisyBody as a Bedrock Converse request: no body
// "model", the model is in the path.
func noisyConverse(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"text": "run it"}}},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"toolUse": map[string]any{"toolUseId": "c1", "name": "shell", "input": map[string]any{}}}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"toolResult": map[string]any{"toolUseId": "c1",
			"content": []any{map[string]any{"text": strings.Repeat("\u001b[32mok\u001b[0m line   \n\n\n", 200)}}}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A Bedrock request's record carries the model its path names, an ARN or
// inference profile whole whether the path arrives escaped or not, so the
// aggregator prices its savings (8xf.16; it read body.model, which Converse
// does not have).
func TestBedrockRecordCarriesPathModel(t *testing.T) {
	for _, path := range []string{
		"/llm/rest/br/model/" + bedrockProfileARN + "/converse",
		"/llm/rest/br/model/arn%3Aaws%3Abedrock%3Aus-east-1%3A123456789012%3Ainference-profile%2Fus.anthropic.claude-3/converse",
	} {
		t.Run(path, func(t *testing.T) {
			p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
			ctx.Vendor = "bedrock"
			rec := &captureRecorder{}
			p.SetRecorder(rec)
			req := request(noisyConverse(t), nil)
			req.Request.Path = path
			if resp, _ := p.HandlePostAuth(ctx, req); !resp.Modified {
				t.Fatal("post_auth did not compress the Converse body")
			}
			body := `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"stopReason":"end_turn","usage":{"inputTokens":2000,"outputTokens":5,"totalTokens":2005}}`
			p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(body)})
			if len(rec.got) != 1 {
				t.Fatalf("records %+v", rec.got)
			}
			r := rec.got[0]
			if r.Model != bedrockProfileARN || r.Vendor != "bedrock" || r.InputTokens != 2000 || r.TokensSaved() <= 0 {
				t.Fatalf("record %+v", r)
			}

			var askedFor string
			agg := records.NewAggregator(func(vendor, model string) (records.Price, bool) {
				askedFor = vendor + " " + model
				return records.Price{Input: 3e-6, Currency: "USD"}, true
			})
			agg.Add(r)
			d := agg.Flush()[0].Deltas[0]
			if askedFor != "bedrock "+bedrockProfileARN || d.CostSaved["USD"] <= 0 || d.UnpricedTokensSaved != 0 {
				t.Errorf("priced for %q: %+v", askedFor, d)
			}
		})
	}
}
