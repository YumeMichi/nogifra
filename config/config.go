package config

import (
	"encoding/json"
	"nogifra/pkg/utils"
	"os"
	"strconv"
	"time"
)

var (
	confFile = "./config.json"
	Conf     = &Configs{}
)

type Export struct {
	AESKey    string `json:"aes_key"`
	OutputDir string `json:"output_dir"`
}

type Fetch struct {
	SecretKey string `json:"secret_key"`
	DeviceID  string `json:"device_id"`
	AppVer    string `json:"app_ver"`
	DLCVer    string `json:"dlc_ver"`
	BytesName string `json:"bytes_name"`
}

type Configs struct {
	DumpDir string `json:"dump_dir"`
	Export  Export `json:"export"`
	Fetch   Fetch  `json:"fetch"`
}

func Init() {
	Conf = loadConf()
}

func defaultConfigs() *Configs {
	return &Configs{
		DumpDir: "dump",
		Export: Export{
			AESKey:    "6533686d39383372684353614c324e50",
			OutputDir: "masterdata",
		},
		Fetch: Fetch{
			SecretKey: "cebd92df-9f97-4308-96f5-630558fc214e",
			DeviceID:  "43438c58-e27b-44ac-99e1-65e6efc5a396",
			AppVer:    "4.9.8",
			DLCVer:    "4.9.8_708a31b71609a8f067583e70eb00035683512559",
			BytesName: "masterdata_basic_216685d034a73bf9d8ef44dcfc64361f02111f3c.bytes",
		},
	}
}

func (c *Configs) normalize() {
	def := defaultConfigs()
	if c.DumpDir == "" {
		c.DumpDir = def.DumpDir
	}
	if c.Export.AESKey == "" {
		c.Export.AESKey = def.Export.AESKey
	}
	if c.Export.OutputDir == "" {
		c.Export.OutputDir = def.Export.OutputDir
	}
	if c.Fetch.SecretKey == "" {
		c.Fetch.SecretKey = def.Fetch.SecretKey
	}
	if c.Fetch.DeviceID == "" {
		c.Fetch.DeviceID = def.Fetch.DeviceID
	}
	if c.Fetch.AppVer == "" {
		c.Fetch.AppVer = def.Fetch.AppVer
	}
	if c.Fetch.DLCVer == "" {
		c.Fetch.DLCVer = def.Fetch.DLCVer
	}
	if c.Fetch.BytesName == "" {
		c.Fetch.BytesName = def.Fetch.BytesName
	}
}

func loadConf() *Configs {
	c := defaultConfigs()
	if !utils.PathExists(confFile) {
		c.normalize()
		_ = c.Save()
		return c
	}
	err := json.Unmarshal(utils.ReadAllText(confFile), c)
	if err != nil {
		_ = os.Rename(confFile, confFile+".backup"+strconv.FormatInt(time.Now().Unix(), 10))
		c.normalize()
		_ = c.Save()
		return c
	}
	c.normalize()
	_ = c.Save()
	return c
}

func SaveConf(c *Configs) error {
	return c.Save()
}

func (c *Configs) Save() error {
	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}
	utils.WriteAllText(confFile, data)
	return nil
}
