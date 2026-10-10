// Package catalog owns the catalogue content: the document model,
// validation against the meta vocabulary, seed import, JSON import/export
// and the cached public catalogue response.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// maxDocumentBytes bounds a catalogue file read by LoadDocument.
const maxDocumentBytes = 64 << 20

// Document is the catalogue in the data/amiga-data.json shape. Sections
// and entries are kept as raw JSON so every content field survives verbatim.
type Document struct {
	Readme  json.RawMessage   `json:"_readme,omitempty"`
	Meta    json.RawMessage   `json:"meta"`
	Domains json.RawMessage   `json:"domains"`
	Tours   json.RawMessage   `json:"tours"`
	Entries []json.RawMessage `json:"entries"`
}

// DecodeDocument parses a catalogue document.
func DecodeDocument(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading catalogue: %w", err)
	}
	if len(data) > maxDocumentBytes {
		return nil, fmt.Errorf("catalogue is larger than %d bytes", maxDocumentBytes)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("catalogue is not valid JSON: %w", err)
	}
	return &doc, nil
}

// LoadDocument reads and parses a catalogue file.
func LoadDocument(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening catalogue: %w", err)
	}
	defer f.Close()
	doc, err := DecodeDocument(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// Encode writes the document as JSON without HTML escaping, indented with
// one space when indent is true (the format of data/amiga-data.json, so an
// export of an unchanged catalogue is byte-identical to the seed).
func (d *Document) Encode(w io.Writer, indent bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", " ")
	}
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("encoding catalogue: %w", err)
	}
	return nil
}

// compact strips insignificant whitespace, keeping key order and content.
func compact(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
