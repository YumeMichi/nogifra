package masterdata

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type SectionSummary struct {
	Index        int               `json:"index"`
	ManagerField string            `json:"manager_field"`
	Type         string            `json:"type"`
	Kind         string            `json:"kind"`
	Count        int               `json:"count,omitempty"`
	BytesStart   int               `json:"bytes_start"`
	BytesEnd     int               `json:"bytes_end"`
	OutputFile   string            `json:"output_file,omitempty"`
	Preview      []map[string]any  `json:"preview,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type ExportSummary struct {
	Input         string           `json:"input"`
	OutputDir     string           `json:"output_dir"`
	Parsed        int              `json:"parsed"`
	Failed        int              `json:"failed"`
	ConsumedBytes int              `json:"consumed_bytes"`
	TotalBytes    int              `json:"total_bytes"`
	Sections      []SectionSummary `json:"sections"`
}

type ExportOptions struct {
	Limit           int
	Preview         int
	ContinueOnError bool
	OutputDir       string
}

type reader struct {
	data []byte
	off  int
}

type rowWriter struct {
	file  *os.File
	first bool
}

func ExportPack(data []byte, schema *Schema, opts ExportOptions) (*ExportSummary, error) {
	classMap := schema.ClassMap()
	enumSet := schema.EnumSet()
	r := &reader{data: data}
	out := &ExportSummary{TotalBytes: len(data)}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	max := len(schema.ManagerFields)
	if opts.Limit > 0 && opts.Limit < max {
		max = opts.Limit
	}

	for i := 0; i < max; i++ {
		field := schema.ManagerFields[i]
		section, err := parseSection(r, field, i, classMap, enumSet, opts)
		if err != nil {
			if !opts.ContinueOnError {
				return nil, fmt.Errorf("parse section %d (%s): %w", i, field.Name, err)
			}
			section = SectionSummary{
				Index:        i,
				ManagerField: field.Name,
				Type:         field.Type,
				BytesStart:   r.off,
				BytesEnd:     r.off,
				Error:        err.Error(),
			}
			out.Failed++
			out.Sections = append(out.Sections, section)
			break
		}
		out.Parsed++
		out.Sections = append(out.Sections, section)
	}

	out.ConsumedBytes = r.off
	return out, nil
}

func parseSection(r *reader, field ManagerField, index int, classes map[string]ClassDef, enums map[string]struct{}, opts ExportOptions) (SectionSummary, error) {
	start := r.off

	switch {
	case strings.HasPrefix(field.Type, "Dictionary<"):
		section, err := parseDictionarySection(r, field, index, classes, enums, opts)
		if err != nil {
			return SectionSummary{}, err
		}
		section.BytesStart = start
		section.BytesEnd = r.off
		return section, nil
	case strings.HasSuffix(field.Type, "[]"):
		section, err := parseArraySection(r, field, index, classes, enums, opts)
		if err != nil {
			return SectionSummary{}, err
		}
		section.BytesStart = start
		section.BytesEnd = r.off
		return section, nil
	default:
		return SectionSummary{}, fmt.Errorf("unsupported manager field type %q", field.Type)
	}
}

func parseArraySection(r *reader, field ManagerField, index int, classes map[string]ClassDef, enums map[string]struct{}, opts ExportOptions) (SectionSummary, error) {
	count, err := r.readInt32()
	if err != nil {
		return SectionSummary{}, err
	}
	if count < 0 {
		return SectionSummary{}, fmt.Errorf("negative array count %d", count)
	}

	elemType := strings.TrimSuffix(field.Type, "[]")
	def, ok := classes[elemType]
	if !ok {
		return SectionSummary{}, fmt.Errorf("missing class definition for %s", elemType)
	}

	outputPath := filepath.Join(opts.OutputDir, fmt.Sprintf("%03d_%s.json", index, field.Name))
	writer, err := newRowWriter(outputPath)
	if err != nil {
		return SectionSummary{}, fmt.Errorf("create output file: %w", err)
	}
	defer writer.close()

	section := SectionSummary{
		Index:        index,
		ManagerField: field.Name,
		Type:         field.Type,
		Kind:         "array",
		Count:        count,
		OutputFile:   outputPath,
	}

	for i := range count {
		row, err := parseObject(r, def, classes, enums)
		if err != nil {
			return SectionSummary{}, fmt.Errorf("row %d: %w", i, err)
		}
		if err := writer.writeRow(row); err != nil {
			return SectionSummary{}, fmt.Errorf("write row %d: %w", i, err)
		}
		if i < opts.Preview {
			section.Preview = append(section.Preview, row)
		}
	}

	if err := writer.finish(); err != nil {
		return SectionSummary{}, err
	}
	return section, nil
}

func parseDictionarySection(r *reader, field ManagerField, index int, classes map[string]ClassDef, enums map[string]struct{}, opts ExportOptions) (SectionSummary, error) {
	count, err := r.readInt32()
	if err != nil {
		return SectionSummary{}, err
	}
	if count < 0 {
		return SectionSummary{}, fmt.Errorf("negative dictionary count %d", count)
	}

	keyType, valueType, err := parseDictionaryTypes(field.Type)
	if err != nil {
		return SectionSummary{}, err
	}

	def, ok := classes[valueType]
	if !ok {
		return SectionSummary{}, fmt.Errorf("missing class definition for %s", valueType)
	}

	outputPath := filepath.Join(opts.OutputDir, fmt.Sprintf("%03d_%s.json", index, field.Name))
	writer, err := newRowWriter(outputPath)
	if err != nil {
		return SectionSummary{}, fmt.Errorf("create output file: %w", err)
	}
	defer writer.close()

	section := SectionSummary{
		Index:        index,
		ManagerField: field.Name,
		Type:         field.Type,
		Kind:         "dictionary",
		Count:        count,
		OutputFile:   outputPath,
		Meta: map[string]string{
			"key_type":   keyType,
			"value_type": valueType,
		},
	}

	for i := range count {
		key, err := readValue(r, keyType, classes, enums)
		if err != nil {
			return SectionSummary{}, fmt.Errorf("dictionary key %d: %w", i, err)
		}
		value, err := parseObject(r, def, classes, enums)
		if err != nil {
			return SectionSummary{}, fmt.Errorf("dictionary value %d: %w", i, err)
		}
		row := value
		row["_key"] = normalizeMapKey(key)
		if err := writer.writeRow(row); err != nil {
			return SectionSummary{}, fmt.Errorf("write dictionary row %d: %w", i, err)
		}
		if i < opts.Preview {
			section.Preview = append(section.Preview, row)
		}
	}

	if err := writer.finish(); err != nil {
		return SectionSummary{}, err
	}
	return section, nil
}

func parseObject(r *reader, def ClassDef, classes map[string]ClassDef, enums map[string]struct{}) (map[string]any, error) {
	if row, ok, err := parseSpecialObject(r, def); ok || err != nil {
		return row, err
	}

	row := make(map[string]any, len(def.Fields))
	for _, field := range def.Fields {
		value, err := readValue(r, field.Type, classes, enums)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", def.Name, field.Name, err)
		}
		row[field.Name] = value
	}
	return row, nil
}

func parseSpecialObject(r *reader, def ClassDef) (map[string]any, bool, error) {
	switch def.Name {
	case "ArcanumData":
		id, err := r.readInt32()
		if err != nil {
			return nil, true, err
		}
		name, err := r.readStringEx()
		if err != nil {
			return nil, true, err
		}
		startAt, err := r.readDateTime()
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"Id": id, "Name": name, "StartAt": startAt}, true, nil
	default:
		return nil, false, nil
	}
}

func readValue(r *reader, typ string, classes map[string]ClassDef, enums map[string]struct{}) (any, error) {
	typ = strings.TrimSpace(typ)

	switch typ {
	case "int":
		return r.readInt32()
	case "long":
		return r.readInt64()
	case "uint":
		return r.readUInt32()
	case "float":
		return r.readFloat32()
	case "double":
		return r.readFloat64()
	case "bool":
		return r.readBool()
	case "string":
		return r.readStringEx()
	case "DateTime":
		return r.readDateTime()
	case "DateTimeOffset":
		return r.readDateTimeOffset()
	case "Nullable<DateTime>":
		return r.readNullableDateTime()
	}

	if inner, ok := splitArrayType(typ); ok {
		return readArrayValue(r, inner, classes, enums)
	}
	if strings.HasPrefix(typ, "Nullable<") && strings.HasSuffix(typ, ">") {
		return nil, fmt.Errorf("unsupported nullable type %s", typ)
	}
	if _, ok := enums[typ]; ok {
		return r.readInt32()
	}
	if def, ok := classes[typ]; ok {
		return parseObject(r, def, classes, enums)
	}

	return nil, fmt.Errorf("unsupported field type %s", typ)
}

func readArrayValue(r *reader, inner string, classes map[string]ClassDef, enums map[string]struct{}) ([]any, error) {
	count, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("negative array count %d", count)
	}

	values := make([]any, 0, count)
	for i := range count {
		v, err := readValue(r, inner, classes, enums)
		if err != nil {
			return nil, fmt.Errorf("array item %d: %w", i, err)
		}
		values = append(values, v)
	}
	return values, nil
}

func splitArrayType(typ string) (string, bool) {
	if !strings.HasSuffix(typ, "[]") {
		return "", false
	}
	return strings.TrimSuffix(typ, "[]"), true
}

func parseDictionaryTypes(typ string) (string, string, error) {
	const prefix = "Dictionary<"
	if !strings.HasPrefix(typ, prefix) || !strings.HasSuffix(typ, ">") {
		return "", "", fmt.Errorf("invalid dictionary type %q", typ)
	}
	inner := typ[len(prefix) : len(typ)-1]
	parts := strings.SplitN(inner, ",", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid dictionary type %q", typ)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func normalizeMapKey(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func newRowWriter(path string) (*rowWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write([]byte("[\n")); err != nil {
		f.Close()
		return nil, err
	}
	return &rowWriter{file: f, first: true}, nil
}

func (w *rowWriter) writeRow(row map[string]any) error {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make(map[string]any, len(row))
	for _, k := range keys {
		ordered[k] = row[k]
	}

	blob, err := json.MarshalIndent(ordered, "", "    ")
	if err != nil {
		return err
	}
	blob = indentBlock(blob, "    ")
	if !w.first {
		if _, err := w.file.Write([]byte(",\n")); err != nil {
			return err
		}
	}
	w.first = false
	_, err = w.file.Write(blob)
	return err
}

func indentBlock(blob []byte, prefix string) []byte {
	lines := bytes.Split(blob, []byte{'\n'})
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		lines[i] = append([]byte(prefix), line...)
	}
	return bytes.Join(lines, []byte{'\n'})
}

func (w *rowWriter) finish() error {
	if _, err := w.file.Write([]byte("\n]\n")); err != nil {
		return err
	}
	return w.file.Close()
}

func (w *rowWriter) close() {
	if w.file != nil {
		_ = w.file.Close()
	}
}

func (r *reader) readInt32() (int, error) {
	if r.off+4 > len(r.data) {
		return 0, ioErr(r.off, 4, len(r.data))
	}
	v := int(int32(binary.LittleEndian.Uint32(r.data[r.off : r.off+4])))
	r.off += 4
	return v, nil
}

func (r *reader) readInt64() (int64, error) {
	if r.off+8 > len(r.data) {
		return 0, ioErr(r.off, 8, len(r.data))
	}
	v := int64(binary.LittleEndian.Uint64(r.data[r.off : r.off+8]))
	r.off += 8
	return v, nil
}

func (r *reader) readUInt32() (uint32, error) {
	if r.off+4 > len(r.data) {
		return 0, ioErr(r.off, 4, len(r.data))
	}
	v := binary.LittleEndian.Uint32(r.data[r.off : r.off+4])
	r.off += 4
	return v, nil
}

func (r *reader) readFloat32() (float32, error) {
	if r.off+4 > len(r.data) {
		return 0, ioErr(r.off, 4, len(r.data))
	}
	v := math.Float32frombits(binary.LittleEndian.Uint32(r.data[r.off : r.off+4]))
	r.off += 4
	return v, nil
}

func (r *reader) readFloat64() (float64, error) {
	if r.off+8 > len(r.data) {
		return 0, ioErr(r.off, 8, len(r.data))
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(r.data[r.off : r.off+8]))
	r.off += 8
	return v, nil
}

func (r *reader) readBool() (bool, error) {
	if r.off+1 > len(r.data) {
		return false, ioErr(r.off, 1, len(r.data))
	}
	v := r.data[r.off]
	r.off++
	return v != 0, nil
}

func (r *reader) readStringEx() (string, error) {
	byteLen, err := r.readInt32()
	if err != nil {
		return "", err
	}
	if byteLen < 0 {
		return "", fmt.Errorf("negative string byte length %d", byteLen)
	}
	if byteLen == 0 {
		return "", nil
	}
	if byteLen%2 != 0 {
		return "", fmt.Errorf("odd utf16 byte length %d", byteLen)
	}
	if r.off+byteLen > len(r.data) {
		return "", ioErr(r.off, byteLen, len(r.data))
	}

	u16 := make([]uint16, byteLen/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(r.data[r.off+i*2 : r.off+i*2+2])
	}
	r.off += byteLen
	return string(utf16.Decode(u16)), nil
}

func (r *reader) readDateTime() (string, error) {
	return r.readStringEx()
}

func (r *reader) readDateTimeOffset() (map[string]any, error) {
	dateData, err := r.readInt64()
	if err != nil {
		return nil, err
	}
	offsetMinutes, err := r.readInt16()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"DateTime":      strconv.FormatInt(dateData, 10),
		"OffsetMinutes": offsetMinutes,
	}, nil
}

func (r *reader) readNullableDateTime() (any, error) {
	hasValue, err := r.readBool()
	if err != nil {
		return nil, err
	}
	if !hasValue {
		return nil, nil
	}
	return r.readDateTime()
}

func (r *reader) readInt16() (int16, error) {
	if r.off+2 > len(r.data) {
		return 0, ioErr(r.off, 2, len(r.data))
	}
	v := int16(binary.LittleEndian.Uint16(r.data[r.off : r.off+2]))
	r.off += 2
	return v, nil
}

func ioErr(off, need, total int) error {
	return fmt.Errorf("unexpected eof at offset %d need %d total %d", off, need, total)
}
