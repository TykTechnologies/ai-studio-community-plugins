// Command mockupstream serves internal/mockvendor, the vendor stand-in for
// the end-to-end run (e2e/phase1.sh) and the live-suite dry runs.
//
//	go run ./e2e/mockupstream -dir /tmp/pc-e2e-requests
//	go run ./e2e/mockupstream -dir /tmp/pc-live -oracle testdata/live/bfcl/oracle.json
//
// It listens on 127.0.0.1:18999 by default: it saves every body it gets.
// Docker Desktop containers reach that as host.docker.internal; on a Linux
// Docker host, where host.docker.internal is the bridge gateway, pass
// -addr with that gateway's address.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/mockvendor"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18999", "listen address")
	dir := flag.String("dir", "mock-requests", "where to record requests")
	oracle := flag.String("oracle", "", "live-suite answers (testdata/live/<suite>/oracle.json): a body naming [task:<id>] gets that task's answer")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	o := mockvendor.Options{Dir: *dir, Logf: log.Printf}
	if *oracle != "" {
		tasks, err := mockvendor.LoadOracle(*oracle)
		if err != nil {
			log.Fatal(err)
		}
		o.Oracle = tasks
	}
	log.Printf("mockupstream on %s recording to %s", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, mockvendor.New(o)))
}
