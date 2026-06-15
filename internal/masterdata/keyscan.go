package masterdata

import (
	"encoding/hex"
	"fmt"
	"os"
)

type KeyMaterial struct {
	Key1Offset      int    `json:"key1_offset"`
	Key2Offset      int    `json:"key2_offset"`
	Key1Hex         string `json:"key1_hex"`
	Key2Hex         string `json:"key2_hex"`
	Key1Printable   string `json:"key1_printable"`
	Key2Printable   string `json:"key2_printable"`
	DerivedKeyHex   string `json:"derived_key_hex"`
	DerivedKeyPrint string `json:"derived_key_printable"`
}

type KeyScanOptions struct {
	Key1Offset int
	Key2Offset int
}

func ExtractKeys(path string, opts KeyScanOptions) (*KeyMaterial, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	key1Off := opts.Key1Offset
	key2Off := opts.Key2Offset
	if key1Off <= 0 {
		key1Off = findKey1Offset(data)
	}
	if key2Off <= 0 {
		key2Off = findKey2Offset(data)
	}
	if key1Off <= 0 || key2Off <= 0 {
		return nil, fmt.Errorf("failed to locate key offsets")
	}
	if key1Off+48 > len(data) || key2Off+16 > len(data) {
		return nil, fmt.Errorf("key offsets out of range")
	}

	key1Bytes := data[key1Off : key1Off+48]
	key2Bytes := data[key2Off : key2Off+16]
	derived, err := deriveAESKey(hex.EncodeToString(key1Bytes), hex.EncodeToString(key2Bytes))
	if err != nil {
		return nil, err
	}

	return &KeyMaterial{
		Key1Offset:      key1Off,
		Key2Offset:      key2Off,
		Key1Hex:         hex.EncodeToString(key1Bytes),
		Key2Hex:         hex.EncodeToString(key2Bytes),
		Key1Printable:   printable(key1Bytes),
		Key2Printable:   printable(key2Bytes),
		DerivedKeyHex:   hex.EncodeToString(derived),
		DerivedKeyPrint: printable(derived),
	}, nil
}

func findKey1Offset(data []byte) int {
	anchor := []byte("AssetBundleWarehouse\\ScrambledAsset.cs\x00\x00\x00")
	for i := 0; i+len(anchor)+48 <= len(data); i++ {
		if !matchBytes(data[i:i+len(anchor)], anchor) {
			continue
		}
		j := i + len(anchor)
		if data[j] == 0 {
			j++
		}
		if j+48 <= len(data) {
			return j
		}
	}
	return -1
}

func findKey2Offset(data []byte) int {
	anchor := []byte("AssetBundleWarehouse\\ScrambledAsset.cs\x00\x00\x00")
	for i := 0; i+len(anchor)+48+16 <= len(data); i++ {
		if !matchBytes(data[i:i+len(anchor)], anchor) {
			continue
		}
		j := i + len(anchor) + 48
		for j < len(data) && data[j] == 0 {
			j++
		}
		if j+16 <= len(data) {
			return j
		}
	}
	return -1
}

func matchBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, v := range b {
		if v >= 32 && v <= 126 {
			out[i] = v
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
