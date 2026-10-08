// Developer-only harness example. Use the prebuilt native fixture for consumer tests.
package main

import (
	"context"
	"encoding/json"
	"github.com/rad1092/lifecase/kit"
	"log"
	"os"
)

func main() {
	r, err := kit.Run(context.Background(), kit.Options{Fixture: os.Getenv("LIFECASE_FIXTURE"), Scenarios: []string{"inherit", "flood"}})
	if err != nil {
		log.Fatal(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(r); err != nil {
		log.Fatal(err)
	}
	if r.Failed() {
		os.Exit(1)
	}
}
