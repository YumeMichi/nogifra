package main

import (
	"fmt"
	"os"
	"path/filepath"

	"nogifra/config"
	"nogifra/internal/masterdata"
)

func main() {
	config.Init()
	dumpCSPath := filepath.Join(config.Conf.DumpDir, "dump.cs")
	finalOut := filepath.Join(config.Conf.DumpDir, "dump.schema.json")

	schema, err := masterdata.LoadSchemaFromDumpCS(dumpCSPath)
	if err != nil {
		fatal(err)
	}
	if err := masterdata.WriteSchema(finalOut, schema); err != nil {
		fatal(err)
	}

	fmt.Printf("dump.cs: %s\n", dumpCSPath)
	fmt.Printf("schema:  %s\n", finalOut)
	fmt.Printf("tables:  %d\n", len(schema.ManagerFields))
	fmt.Printf("classes: %d\n", len(schema.Classes))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
