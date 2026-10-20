package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
)

// StrategyVersion is the version of the compression strategies. Bump it
// whenever a strategy's output changes for the same input, so every policy
// hash changes with it and records show which rules changed a request.
//
//	1: level 1 (lossless) strategies
//	2: level 2 test-runner reducers (compress/reducers.go registry
//	   pytest@1,jest@1,go test@1,cargo test@1,maven@1,gradle@1)
//	3: level 2 log and listing reducers (terraform plan, kubectl, git diff,
//	   grep, listing, build log, stack trace, repeated lines)
const StrategyVersion = 3

// Hash returns the policy hash carried on every savings record: the SHA-256
// of the policy's canonical JSON (its effective settings, defaults filled
// in) and the strategy version. It is stable across key order in the admin's
// JSON and changes when any setting changes.
//
// Engine credentials are not part of it: Studio sees a $SECRET/ reference
// where an edge sees the value, and the hash must agree on both.
func (p *Policy) Hash() string {
	if p.hash != "" {
		return p.hash
	}
	return p.computeHash()
}

// ShortHash is the first 12 hex digits of Hash, for headers and the UI.
func (p *Policy) ShortHash() string {
	return p.Hash()[:12]
}

func (p *Policy) computeHash() string {
	canon := *p
	canon.Roles = append([]string(nil), p.Roles...)
	sort.Strings(canon.Roles)
	if canon.Engine != nil {
		e := *canon.Engine
		if e.TimeoutMs == 0 {
			e.TimeoutMs = DefaultEngineTimeout
		}
		canon.Engine = &e
	}
	// encoding/json writes struct fields in declaration order and map keys
	// sorted, so the encoding is canonical.
	body, err := json.Marshal(&canon)
	if err != nil {
		// Only unsupported values fail, and a Policy has none
		panic("config: policy does not encode: " + err.Error())
	}
	h := sha256.New()
	h.Write([]byte("prompt-compressor/policy/v" + strconv.Itoa(StrategyVersion) + "\n"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// sealHashes fills the cached hash of every policy. Validate calls it once
// the config is valid.
func (c *Config) sealHashes() {
	for _, p := range c.Policies {
		p.hash = p.computeHash()
	}
}

func init() {
	offPolicy.hash = offPolicy.computeHash()
	defaultPolicy.hash = defaultPolicy.computeHash()
}
