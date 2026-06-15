# nogifra

Tools for fetching, decrypting, and exporting Nogifra masterdata into readable JSON.

## Tools

- `masterdata-fetch`: Call `access_token` and `dlc_version`, fetch the DLC index, locate `MasterData/basic.bytes`, and download the raw masterdata bytes into `<dump_dir>`
- `masterdata-key`: Read `<dump_dir>/global-metadata.dat`, extract `aes_key`, and write `<dump_dir>/keys.json`
- `masterdata-schema`: Read `<dump_dir>/dump.cs` and generate `<dump_dir>/dump.schema.json`
- `masterdata-export`: Read config plus files under `<dump_dir>`, then decrypt, decompress, and export readable masterdata JSON

## Fetch Raw Masterdata

The root entrypoint reproduces the captured client flow:

1. `POST /das/access_token`
2. `POST /api/environment/dlc_version`
3. `GET /DlcAssets/android-ja/indexes/<dlc_ver>`
4. Find the entry where `p == "MasterData/basic.bytes"`
5. `GET /DlcAssets/android-ja/scrambles/<n>`
6. Save the downloaded bytes into `<dump_dir>`

Example:

```bash
go run ./cmd/masterdata-fetch
```

Minimal `config.json`:

```json
{
    "dump_dir": "dump",
    "export": {
        "aes_key": "6533686d39383372684353614c324e50",
        "output_dir": "masterdata"
    },
    "fetch": {
        "secret_key": "cebd92df-9f97-4308-96f5-630558fc214e",
        "device_id": "43438c58-e27b-44ac-99e1-65e6efc5a396",
        "app_ver": "4.9.8",
        "dlc_ver": "4.9.8_708a31b71609a8f067583e70eb00035683512559",
        "bytes_name": "masterdata_basic_216685d034a73bf9d8ef44dcfc64361f02111f3c.bytes"
    }
}
```

Optional flags:

- `-timeout`: HTTP timeout

`masterdata-fetch` reads `secret_key`, `device_id`, `app_ver`, and `dump_dir` directly from `config.json`, then writes back the latest `dlc_ver` and `bytes_name`.
`masterdata-export` uses `fetch.bytes_name` as the input file and does not scan for multiple bytes files.
Defaults currently match the captured `4.9.8` client request sample.

## Update Workflow

1. Put the XAPK-extracted `global-metadata.dat` and the Il2CppDumper `dump.cs` into `<dump_dir>`.
2. Run:
   - `go run ./cmd/masterdata-fetch`
3. Run:
   - `go run ./cmd/masterdata-key`
4. Run:
   - `go run ./cmd/masterdata-schema`
5. Run:
   - `go run ./cmd/masterdata-export`

`masterdata-fetch` should run before `masterdata-export` so the latest bytes name is stored in `config.json`. `masterdata-key`, `masterdata-schema`, and `masterdata-export` no longer take file path arguments. They all read from `config.json` plus files under `<dump_dir>`.

## Decode Flow

`bytes -> gzip -> AES-CBC/PKCS7 -> optional lz4b -> pack -> json`

More explicitly:

1. Read raw `masterdata_basic_*.bytes`
2. Gunzip the whole file
3. AES-CBC decrypt the result
   - IV = first 16 bytes of the gzipped payload
   - key = derived runtime AES key
4. Remove PKCS7 padding
5. If plaintext starts with `lz4b`:
   - bytes `0x00..0x03` = `lz4b`
   - bytes `0x04..0x07` = little-endian decompressed size
   - bytes `0x08..` = LZ4 block payload
6. Parse the resulting masterdata pack with the schema derived from `<dump_dir>/dump.cs`

## Key Recovery

- The following confirmed key material applies to masterdata version `4.9.8_708a31b71609a8f067583e70eb00035683512559`
- On every client update, re-extract keys from the updated `global-metadata.dat`; do not assume the previous version's keys still apply
- `key1` and `key2` come from the XAPK-extracted `global-metadata.dat` in `<dump_dir>`
- For this sample they were found at:
  - `key1` @ `0x9DBEE8`
  - `key2` @ `0x9DBF20`
- `key1` is a 48-byte AES-CBC ciphertext blob
- `key2` is the 16-byte AES key used to decrypt `key1`
- The IV for that CBC step is the first 16 bytes of `key1`
- The decrypted `key1` becomes the final AES key for masterdata
- For this sample the derived AES key is `e3hm983rhCSaL2NP`
- For easier comparison with `global-metadata.dat`, keep both hex and printable views:
  - `key1` hex: `df09534aefaaddc051aa84474c1af016dac2d6cf1e5cb73e918c720825e5c323a53d6d17de0036f663b5248ceefa7ea6`
  - `key1` printable: `..SJ....Q..GL........\.>..r.%..#.=m...6.c.$...~.`
  - `key2` hex: `3e7043502d6d7c5629583a283345256b`
  - `key2` printable: `>pCP-m|V)X:(3E%k`
  - derived AES key hex: `6533686d39383372684353614c324e50`
  - derived AES key printable: `e3hm983rhCSaL2NP`
- Printable form is for inspection only; tool input still uses hex
- `go run ./cmd/masterdata-key` prints JSON with offsets, hex, and printable views
- In normal use, prefer `derived_key_hex` from `go run ./cmd/masterdata-key`

### How To Re-find Keys On Update

When the game updates, assume offsets may move.

1. Extract the new `global-metadata.dat` from the XAPK into `<dump_dir>`
2. Look for the same key material region used by the previous version
3. Verify candidates with these constraints:
   - `key2` should be 16 bytes
   - `key1` should decrypt cleanly with AES-CBC + PKCS7
   - the resulting plaintext should be a valid AES key string/blob
   - using that derived key, `masterdata_basic_*.bytes` should decrypt successfully
   - the decrypted payload should usually begin with `lz4b` or directly with the pack body
4. Once the new values are confirmed, run `go run ./cmd/masterdata-key` to refresh `config.json` automatically.
5. `go run ./cmd/masterdata-key` automates this search for the current observed layout, but if the metadata layout changes heavily in a future version, the scanner may need to be adjusted.

### Practical Validation

The quickest validation loop is:

1. Regenerate `<dump_dir>/dump.schema.json` from the updated `<dump_dir>/dump.cs`
2. Try exporting with the candidate keys
3. Confirm that:
   - table count is reasonable
   - early tables decode to readable UTF-16 strings
   - `_summary.json` shows no parse failures
