package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	managerFieldLineRe = regexp.MustCompile(`^\s*private\s+(.+?)\s+([A-Za-z0-9_]+)_; // 0x([0-9A-Fa-f]+)$`)
	publicFieldLineRe  = regexp.MustCompile(`^\s*public\s+(.+?)\s+([A-Za-z0-9_<>]+); // 0x([0-9A-Fa-f]+)$`)
	classLineRe        = regexp.MustCompile(`^public class ([A-Za-z0-9_<>.]+)(?:\s|:)`)
	enumLineRe         = regexp.MustCompile(`^public enum ([A-Za-z0-9_<>.]+)(?:\s|$)`)
)

type ManagerField struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Offset uint64 `json:"offset"`
}

type ClassField struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
}

type ClassDef struct {
	Name      string       `json:"name"`
	Fields    []ClassField `json:"fields"`
	TableName string       `json:"table_name,omitempty"`
}

type Schema struct {
	ManagerFields []ManagerField `json:"manager_fields"`
	Classes       []ClassDef     `json:"classes"`
	Enums         []string       `json:"enums"`
}

func LoadSchema(path string) (*Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var schema Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("parse schema json: %w", err)
	}
	schema.Normalize()
	if len(schema.ManagerFields) == 0 {
		return nil, errors.New("schema has no manager fields")
	}
	return &schema, nil
}

func WriteSchema(path string, schema *Schema) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create schema dir: %w", err)
	}
	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func LoadSchemaFromDumpCS(path string) (*Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dump.cs: %w", err)
	}

	lines := strings.Split(string(raw), "\n")
	managerFields, err := loadManagerFields(lines)
	if err != nil {
		return nil, err
	}
	classes, enums, err := loadClassAndEnumDefs(lines)
	if err != nil {
		return nil, err
	}

	schema := &Schema{
		ManagerFields: managerFields,
		Classes:       classes,
		Enums:         enums,
	}
	schema.Normalize()
	return schema, nil
}

func (s *Schema) Normalize() {
	sort.Slice(s.ManagerFields, func(i, j int) bool { return s.ManagerFields[i].Offset < s.ManagerFields[j].Offset })
	sort.Slice(s.Classes, func(i, j int) bool { return s.Classes[i].Name < s.Classes[j].Name })
	sort.Strings(s.Enums)
	for i := range s.Classes {
		sort.Slice(s.Classes[i].Fields, func(a, b int) bool {
			return s.Classes[i].Fields[a].Offset < s.Classes[i].Fields[b].Offset
		})
	}
}

func (s *Schema) ClassMap() map[string]ClassDef {
	out := make(map[string]ClassDef, len(s.Classes))
	for _, class := range s.Classes {
		out[class.Name] = class
	}
	return out
}

func (s *Schema) EnumSet() map[string]struct{} {
	out := make(map[string]struct{}, len(s.Enums))
	for _, name := range s.Enums {
		out[name] = struct{}{}
	}
	return out
}

func loadManagerFields(lines []string) ([]ManagerField, error) {
	var fields []ManagerField
	inManager := false
	inFields := false

	for _, line := range lines {
		if strings.HasPrefix(line, "public class MasterDataManager : MonoBehaviour") {
			inManager = true
			continue
		}
		if !inManager {
			continue
		}
		if strings.TrimSpace(line) == "// Fields" {
			inFields = true
			continue
		}
		if inFields && strings.TrimSpace(line) == "// Properties" {
			break
		}
		if !inFields {
			continue
		}

		m := managerFieldLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		offset, err := strconv.ParseUint(m[3], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parse manager field offset %q: %w", m[3], err)
		}
		fields = append(fields, ManagerField{
			Name:   m[2],
			Type:   strings.TrimSpace(m[1]),
			Offset: offset,
		})
	}

	if len(fields) == 0 {
		return nil, errors.New("no MasterDataManager fields found")
	}
	return fields, nil
}

func loadClassAndEnumDefs(lines []string) ([]ClassDef, []string, error) {
	var classes []ClassDef
	var enums []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if m := enumLineRe.FindStringSubmatch(line); m != nil {
			enums = append(enums, m[1])
			continue
		}

		m := classLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		name := m[1]
		def := ClassDef{Name: name}
		inFields := false

		for i = i + 1; i < len(lines); i++ {
			line = lines[i]
			trimmed := strings.TrimSpace(line)

			if trimmed == "// Fields" {
				inFields = true
				continue
			}
			if strings.HasPrefix(trimmed, "// Methods") {
				break
			}
			if strings.Contains(line, `public const string TableName = "`) {
				start := strings.Index(line, `"`)
				end := strings.LastIndex(line, `"`)
				if start >= 0 && end > start {
					def.TableName = line[start+1 : end]
				}
			}
			if !inFields {
				continue
			}

			fm := publicFieldLineRe.FindStringSubmatch(line)
			if fm == nil {
				continue
			}
			fieldType := strings.TrimSpace(fm[1])
			if strings.Contains(line, "const string TableName") || strings.HasPrefix(fieldType, "const ") || strings.HasPrefix(fieldType, "static ") {
				continue
			}

			offset, err := strconv.ParseUint(fm[3], 16, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("parse class field offset %q: %w", fm[3], err)
			}

			def.Fields = append(def.Fields, ClassField{
				Type:   fieldType,
				Name:   fm[2],
				Offset: offset,
			})
		}

		if def.TableName != "" {
			classes = append(classes, def)
		}
	}

	if len(classes) == 0 {
		return nil, nil, errors.New("no master data classes found")
	}

	return classes, enums, nil
}
