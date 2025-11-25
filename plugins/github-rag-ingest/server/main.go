package main

import (
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

func main() {
	plugin_sdk.Serve(NewGitHubRAGPlugin())
}
