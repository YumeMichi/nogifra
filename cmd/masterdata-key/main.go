package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nogifra/config"
	"nogifra/internal/masterdata"
)

func main() {
	config.Init()
	inPath := filepath.Join(config.Conf.DumpDir, "global-metadata.dat")
	outPath := filepath.Join(config.Conf.DumpDir, "keys.json")

	keys, err := masterdata.ExtractKeys(inPath, masterdata.KeyScanOptions{})
	if err != nil {
		fatal(err)
	}

	raw, err := json.MarshalIndent(keys, "", "    ")
	if err != nil {
		fatal(err)
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(outPath, raw, 0o644); err != nil {
		fatal(fmt.Errorf("write output: %w", err))
	}
	config.Conf.Export.AESKey = keys.DerivedKeyHex
	_ = config.SaveConf(config.Conf)
	fmt.Printf("metadata: %s\n", inPath)
	fmt.Printf("output:   %s\n", outPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
