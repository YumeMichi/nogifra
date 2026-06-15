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
	inPath, err := defaultInputPath(config.Conf.DumpDir, config.Conf.Fetch.BytesName)
	if err != nil {
		fatal(err)
	}
	schemaPath := filepath.Join(config.Conf.DumpDir, "dump.schema.json")
	outDir := config.Conf.Export.OutputDir
	aesKey := config.Conf.Export.AESKey

	if aesKey == "" {
		fatal(fmt.Errorf("export.aes_key is empty; run masterdata-key first"))
	}

	plain, err := decodeInput(inPath, aesKey)
	if err != nil {
		fatal(err)
	}
	schema, err := loadSchema(schemaPath)
	if err != nil {
		fatal(err)
	}

	finalOut := outDir
	if err := os.MkdirAll(finalOut, 0o755); err != nil {
		fatal(fmt.Errorf("create output dir: %w", err))
	}
	if err := clearOutputDir(finalOut); err != nil {
		fatal(err)
	}

	summary, err := masterdata.ExportPack(plain, schema, masterdata.ExportOptions{
		Preview:   1,
		OutputDir: finalOut,
	})
	if err != nil {
		fatal(err)
	}

	summary.Input = inPath
	summary.OutputDir = finalOut

	summaryPath := filepath.Join(finalOut, "_summary.json")
	if err := writeSummary(summaryPath, summary); err != nil {
		fatal(err)
	}

	config.Conf.Export.OutputDir = finalOut
	if resolvedKey, err := masterdata.ResolveAESKeyHex(masterdata.DecodeConfig{AESKeyHex: aesKey}); err == nil {
		config.Conf.Export.AESKey = resolvedKey
	}
	_ = config.SaveConf(config.Conf)

	fmt.Printf("input:      %s\n", inPath)
	fmt.Printf("output dir: %s\n", finalOut)
	fmt.Printf("summary:    %s\n", summaryPath)
	fmt.Printf("parsed:     %d\n", summary.Parsed)
	fmt.Printf("failed:     %d\n", summary.Failed)
	fmt.Printf("consumed:   %d / %d bytes\n", summary.ConsumedBytes, summary.TotalBytes)
}

func decodeInput(path, aesKey string) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return masterdata.DecodeFile(src, masterdata.DecodeConfig{AESKeyHex: aesKey})
}

func defaultInputPath(dumpDir, bytesName string) (string, error) {
	if bytesName == "" {
		return "", fmt.Errorf("fetch.bytes_name is empty; run masterdata-fetch first or pass -in explicitly")
	}
	return filepath.Join(dumpDir, bytesName), nil
}

func loadSchema(schemaPath string) (*masterdata.Schema, error) {
	return masterdata.LoadSchema(schemaPath)
}

func writeSummary(path string, summary *masterdata.ExportSummary) error {
	raw, err := json.MarshalIndent(summary, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func clearOutputDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read output dir: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return fmt.Errorf("clear output entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
