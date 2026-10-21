package eval

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// maxGatewayBody bounds a request body CompressingGateway reads.
const maxGatewayBody = 64 << 20

// CompressingGateway stands in for a Microgateway in tests and local dry
// runs: it serves /llm/rest/<slug>/<vendor path>, compresses the body with
// the slug's policy the way the plugin's post_auth does (enforce mode,
// one compressor across requests, so its memo behaves as an edge's), and
// hands the request to upstream at the vendor path. A slug with no policy
// (or a nil one) forwards the body unchanged: compression off.
func CompressingGateway(upstream http.Handler, policies map[string]*config.Policy) http.Handler {
	c := compress.New(compress.Options{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/llm/rest/")
		slug, tail, found := strings.Cut(rest, "/")
		if !ok || !found || slug == "" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxGatewayBody))
		if err != nil {
			http.Error(w, "reading the body", http.StatusBadRequest)
			return
		}
		if p := policies[slug]; p != nil {
			format := FormatOpenAI
			if strings.HasSuffix(tail, "messages") {
				format = FormatAnthropic
			}
			out, rep := c.Compress(compress.Input{Format: format, Body: body, Policy: p})
			if rep.Outcome == compress.OutcomeCompressed {
				body = out
				w.Header().Set("X-Tyk-Compression", "applied")
				w.Header().Set("X-Tyk-Compression-Tokens-Saved", strconv.Itoa(rep.TokensSaved()))
			}
		}
		up := r.Clone(r.Context())
		up.URL.Path = "/" + tail
		up.Body = io.NopCloser(bytes.NewReader(body))
		up.ContentLength = int64(len(body))
		upstream.ServeHTTP(w, up)
	})
}
