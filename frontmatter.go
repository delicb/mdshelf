package main

import (
	"bytes"
	"fmt"
	"html"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type metadataField struct {
	name  string
	value string
}

type frontMatterData struct {
	body   []byte
	title  string
	fields []metadataField
	values map[string]any
}

func extractFrontMatter(source []byte) (frontMatterData, error) {
	result := frontMatterData{body: source}
	firstLine, next := frontMatterLine(source, 0)
	delimiter := string(bytes.TrimSuffix(firstLine, []byte{'\r'}))
	if delimiter != "---" && delimiter != "+++" {
		return result, nil
	}

	closingStart := -1
	bodyStart := -1
	for position := next; position <= len(source); {
		line, following := frontMatterLine(source, position)
		if string(bytes.TrimSuffix(line, []byte{'\r'})) == delimiter {
			closingStart = position
			bodyStart = following
			break
		}
		if following <= position {
			break
		}
		position = following
	}
	if closingStart < 0 {
		return result, nil
	}

	values := map[string]any{}
	raw := source[next:closingStart]
	var err error
	if delimiter == "---" {
		err = yaml.Unmarshal(raw, &values)
	} else {
		_, err = toml.Decode(string(raw), &values)
	}
	if err != nil {
		return frontMatterData{}, fmt.Errorf("parse front matter: %w", err)
	}

	result.body = source[bodyStart:]
	result.values = values
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
	for _, key := range keys {
		value := formatMetadataValue(values[key])
		if strings.EqualFold(key, "title") {
			result.title = value
			continue
		}
		if value != "" {
			result.fields = append(result.fields, metadataField{name: metadataLabel(key), value: value})
		}
	}
	return result, nil
}

func frontMatterLine(source []byte, start int) ([]byte, int) {
	if start >= len(source) {
		return source[len(source):], len(source)
	}
	stop := bytes.IndexByte(source[start:], '\n')
	if stop < 0 {
		return source[start:], len(source)
	}
	stop += start
	return source[start:stop], stop + 1
}

func formatMetadataValue(value any) string {
	if value == nil {
		return ""
	}
	if instant, ok := value.(time.Time); ok {
		if instant.Hour() == 0 && instant.Minute() == 0 && instant.Second() == 0 && instant.Nanosecond() == 0 {
			return instant.Format(time.DateOnly)
		}
		return instant.Format(time.RFC3339)
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Array, reflect.Slice:
		parts := make([]string, 0, reflected.Len())
		for i := 0; i < reflected.Len(); i++ {
			if part := formatMetadataValue(reflected.Index(i).Interface()); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, ", ")
	case reflect.Map:
		keys := reflected.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface()) })
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%v: %s", key.Interface(), formatMetadataValue(reflected.MapIndex(key).Interface())))
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(value)
	}
}

func metadataLabel(key string) string {
	parts := strings.FieldsFunc(key, func(character rune) bool { return character == '-' || character == '_' || character == ' ' })
	for index, part := range parts {
		if part == "" {
			continue
		}
		characters := []rune(part)
		characters[0] = unicode.ToUpper(characters[0])
		parts[index] = string(characters)
	}
	return strings.Join(parts, " ")
}

func injectMetadata(renderedHTML string, fields []metadataField) string {
	if len(fields) == 0 {
		return renderedHTML
	}
	var panel strings.Builder
	panel.WriteString(`<section class="document-metadata" aria-label="Document metadata"><dl>`)
	for _, field := range fields {
		panel.WriteString(`<div><dt>`)
		panel.WriteString(html.EscapeString(field.name))
		panel.WriteString(`</dt><dd>`)
		panel.WriteString(html.EscapeString(field.value))
		panel.WriteString(`</dd></div>`)
	}
	panel.WriteString(`</dl></section>`)

	position := strings.Index(renderedHTML, "</h1>")
	if position < 0 {
		return panel.String() + "\n" + renderedHTML
	}
	position += len("</h1>")
	return renderedHTML[:position] + "\n" + panel.String() + renderedHTML[position:]
}
