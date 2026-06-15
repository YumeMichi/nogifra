package masterdata

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const Signature = "lz4b"

type DecodeConfig struct {
	AESKeyHex string
	Key1Hex   string
	Key2Hex   string
}

func DecodeFile(src []byte, cfg DecodeConfig) ([]byte, error) {
	gzPlain := src
	if plain, err := gunzipBytes(src); err == nil {
		gzPlain = plain
	}

	aesKey, err := decodeAESKey(cfg)
	if err != nil {
		return nil, err
	}

	plain, err := decryptCBC(gzPlain, aesKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt payload: %w", err)
	}

	if len(plain) >= 8 && string(plain[:4]) == Signature {
		size := binary.LittleEndian.Uint32(plain[4:8])
		decoded, err := decompressLZ4Block(plain[8:], int(size))
		if err != nil {
			return nil, fmt.Errorf("lz4b decompress: %w", err)
		}
		return decoded, nil
	}

	return plain, nil
}

func ResolveAESKeyHex(cfg DecodeConfig) (string, error) {
	aesKey, err := decodeAESKey(cfg)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(aesKey), nil
}

func decodeAESKey(cfg DecodeConfig) ([]byte, error) {
	if cfg.AESKeyHex != "" {
		aesKey, err := hex.DecodeString(cfg.AESKeyHex)
		if err != nil {
			return nil, fmt.Errorf("decode aes key: %w", err)
		}
		return aesKey, nil
	}
	aesKey, err := deriveAESKey(cfg.Key1Hex, cfg.Key2Hex)
	if err != nil {
		return nil, fmt.Errorf("derive aes key: %w", err)
	}
	return aesKey, nil
}

func deriveAESKey(key1Hex, key2Hex string) ([]byte, error) {
	key1, err := hex.DecodeString(key1Hex)
	if err != nil {
		return nil, fmt.Errorf("decode key1: %w", err)
	}
	key2, err := hex.DecodeString(key2Hex)
	if err != nil {
		return nil, fmt.Errorf("decode key2: %w", err)
	}
	return decryptCBC(key1, key2)
}

func gunzipBytes(src []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func decryptCBC(src, key []byte) ([]byte, error) {
	if len(src) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short: %d", len(src))
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid aes key size: %d", len(key))
	}

	iv := src[:aes.BlockSize]
	ciphertext := src[aes.BlockSize:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext length: %d", len(ciphertext))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	return pkcs7Unpad(plain, aes.BlockSize)
}

func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 || len(src)%blockSize != 0 {
		return nil, errors.New("invalid padded data length")
	}

	pad := int(src[len(src)-1])
	if pad == 0 || pad > blockSize || pad > len(src) {
		return nil, fmt.Errorf("invalid padding size: %d", pad)
	}

	for _, b := range src[len(src)-pad:] {
		if int(b) != pad {
			return nil, errors.New("invalid pkcs7 padding bytes")
		}
	}

	return src[:len(src)-pad], nil
}

func decompressLZ4Block(src []byte, expectedSize int) ([]byte, error) {
	dst := make([]byte, 0, expectedSize)
	i := 0

	for i < len(src) {
		token := int(src[i])
		i++

		litLen := token >> 4
		if litLen == 15 {
			extra, next, err := readLZ4Length(src, i)
			if err != nil {
				return nil, err
			}
			litLen += extra
			i = next
		}

		if i+litLen > len(src) {
			return nil, errors.New("literal overruns input")
		}
		dst = append(dst, src[i:i+litLen]...)
		i += litLen

		if i >= len(src) {
			break
		}
		if i+2 > len(src) {
			return nil, errors.New("missing match offset")
		}

		offset := int(binary.LittleEndian.Uint16(src[i : i+2]))
		i += 2
		if offset <= 0 || offset > len(dst) {
			return nil, fmt.Errorf("invalid match offset: %d", offset)
		}

		matchLen := token & 0x0f
		if matchLen == 15 {
			extra, next, err := readLZ4Length(src, i)
			if err != nil {
				return nil, err
			}
			matchLen += extra
			i = next
		}
		matchLen += 4

		start := len(dst) - offset
		for j := 0; j < matchLen; j++ {
			dst = append(dst, dst[start+j])
		}
	}

	if len(dst) != expectedSize {
		return nil, fmt.Errorf("unexpected lz4 size: got %d want %d", len(dst), expectedSize)
	}

	return dst, nil
}

func readLZ4Length(src []byte, i int) (int, int, error) {
	total := 0
	for {
		if i >= len(src) {
			return 0, i, errors.New("truncated lz4 length")
		}
		b := int(src[i])
		i++
		total += b
		if b != 255 {
			return total, i, nil
		}
	}
}
