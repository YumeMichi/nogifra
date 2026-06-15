package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nogifra/config"
)

const (
	appBaseURL    = "https://production-app.delta.gu3.jp"
	dlcIndexBase  = "https://production-dlc.delta.gu3.jp/DlcAssets/android-ja/indexes"
	dlcDataBase   = "https://production-dlc.delta.gu3.jp/DlcAssets/android-ja/scrambles"
	deviceOS      = "android"
	storePlatform = "googleplay"
	unityVersion  = "2022.3.67f2"
	userAgent     = "UnityPlayer/2022.3.67f2 (UnityWebRequest/1.0, libcurl/8.10.1-DEV)"
	gumiUserAgent = `{"os_info":"Android OS 12 / API-32 (V417IR/1518)","device_model":"Xiaomi 24031PN0DC","cpu_info":"x86-64","graphics_device_vendor":"Qualcomm","graphics_device_model":"Adreno (TM) 640","memory_size":"3940MB"}`
)

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type dlcVersionResponse struct {
	DLCVer string `json:"dlc_ver"`
	Option string `json:"option"`
}

func main() {
	config.Init()
	timeout := flag.Duration("timeout", 30*time.Second, "request timeout")
	flag.Parse()

	if err := run(config.Conf.Fetch.SecretKey, config.Conf.Fetch.DeviceID, config.Conf.Fetch.AppVer, config.Conf.DumpDir, *timeout); err != nil {
		fatal(err)
	}
}

func run(secretKey, deviceID, appVer, dumpDir string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

	accessToken, err := fetchAccessToken(client, secretKey, deviceID, appVer)
	if err != nil {
		return err
	}

	dlcVer, err := fetchDLCVersion(client, accessToken, appVer)
	if err != nil {
		return err
	}

	indexURL := fmt.Sprintf("%s/%s", dlcIndexBase, url.PathEscape(dlcVer))
	indexBody, err := doGET(client, indexURL, accessToken, appVer, nil)
	if err != nil {
		return err
	}

	assetName, err := findAssetName(indexBody, "MasterData/basic.bytes")
	if err != nil {
		return err
	}

	dataURL := fmt.Sprintf("%s/%s", dlcDataBase, url.PathEscape(assetName))
	dataBody, err := doGET(client, dataURL, accessToken, appVer, nil)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		return fmt.Errorf("create dump dir: %w", err)
	}
	outPath := filepath.Join(dumpDir, filepath.Base(assetName))
	if err := os.WriteFile(outPath, dataBody, 0o644); err != nil {
		return fmt.Errorf("write bytes: %w", err)
	}

	config.Conf.Fetch.SecretKey = secretKey
	config.Conf.Fetch.DeviceID = deviceID
	config.Conf.Fetch.AppVer = appVer
	config.Conf.Fetch.DLCVer = dlcVer
	config.Conf.Fetch.BytesName = filepath.Base(assetName)
	_ = config.SaveConf(config.Conf)

	fmt.Println("access_token:", accessToken)
	fmt.Println("dlc_ver:", dlcVer)
	fmt.Println("asset:", assetName)
	fmt.Println("saved:", outPath)
	return nil
}

func fetchAccessToken(client *http.Client, secretKey, deviceID, appVer string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"secret_key": secretKey,
		"device_id":  deviceID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal access_token body: %w", err)
	}
	raw, err := doPOST(client, appBaseURL+"/das/access_token", "", appVer, body)
	if err != nil {
		return "", err
	}
	var resp accessTokenResponse
	if err := unmarshalJSON(raw, &resp); err != nil {
		return "", fmt.Errorf("parse access_token response: %w", err)
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token response")
	}
	return resp.AccessToken, nil
}

func fetchDLCVersion(client *http.Client, accessToken, appVer string) (string, error) {
	raw, err := doPOST(client, appBaseURL+"/api/environment/dlc_version", accessToken, appVer, []byte("{}"))
	if err != nil {
		return "", err
	}
	var resp dlcVersionResponse
	if err := unmarshalJSON(raw, &resp); err != nil {
		return "", fmt.Errorf("parse dlc_version response: %w", err)
	}
	if resp.DLCVer == "" {
		return "", fmt.Errorf("empty dlc_ver response")
	}
	return resp.DLCVer, nil
}

func doPOST(client *http.Client, rawURL, accessToken, appVer string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	setCommonHeaders(req, accessToken, appVer)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return doRequest(client, req)
}

func doGET(client *http.Client, rawURL, accessToken, appVer string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	setCommonHeaders(req, accessToken, appVer)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return doRequest(client, req)
}

func setCommonHeaders(req *http.Request, accessToken, appVer string) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "text/plain")
	req.Header.Set("X-GUMI-USER-AGENT", gumiUserAgent)
	req.Header.Set("X-GUMI-REQUEST-ID", newRequestID())
	req.Header.Set("X-GUMI-DEVICE-OS", deviceOS)
	req.Header.Set("X-GUMI-STORE-PLATFORM", storePlatform)
	req.Header.Set("X-GUMI-APP-VER", appVer)
	req.Header.Set("X-Unity-Version", unityVersion)
	if accessToken == "" {
		req.Header.Set("Authorization", "das")
		return
	}
	req.Header.Set("Authorization", "das "+accessToken)
}

func doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do %s %s: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()
	raw, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", req.Method, req.URL.String(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: status %s: %s", req.Method, req.URL.String(), resp.Status, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	return raw, nil
}

func unmarshalJSON(raw []byte, dst any) error {
	raw = bytes.TrimSpace(raw)
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	return json.Unmarshal(raw, dst)
}

func findAssetName(raw []byte, targetP string) (string, error) {
	var v any
	if err := unmarshalJSON(raw, &v); err != nil {
		return "", fmt.Errorf("parse index json: %w", err)
	}
	if name, ok := walkAssetName(v, targetP); ok {
		return name, nil
	}
	return "", fmt.Errorf("asset %q not found in index json", targetP)
}

func walkAssetName(v any, targetP string) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		if p, ok := x["p"].(string); ok && p == targetP {
			if n, ok := x["n"].(string); ok && n != "" {
				return n, true
			}
		}
		for _, child := range x {
			if name, ok := walkAssetName(child, targetP); ok {
				return name, true
			}
		}
	case []any:
		for _, child := range x {
			if name, ok := walkAssetName(child, targetP); ok {
				return name, true
			}
		}
	}
	return "", false
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
