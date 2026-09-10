package spatial

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
)

// validateVisualAssetContent performs minimal but real structural
// validation on published asset bytes — not a full glTF-conformance
// validator, but genuinely inspecting the bytes rather than trusting the
// declared content type. RP4D §3/§4: a published asset must be a single,
// self-contained object; a GLB referencing an external buffer/image file,
// or a USDZ using deflate instead of stored compression, is rejected
// outright.
func validateVisualAssetContent(format VisualAssetFormat, content []byte) error {
	if len(content) == 0 {
		return ErrInvalidVisualAssetContent
	}
	switch format {
	case VisualAssetFormatGLB:
		return validateGLBContent(content)
	case VisualAssetFormatUSDZ:
		return validateUSDZContent(content)
	default:
		return ErrInvalidVisualAssetContent
	}
}

// glbHeaderSize is the fixed 12-byte GLB header: 4-byte magic ("glTF"),
// uint32 version, uint32 total declared length (all little-endian).
const glbHeaderSize = 12

// glbChunkHeaderSize is the fixed 8-byte chunk header preceding every GLB
// chunk: uint32 chunk length, 4-byte chunk type ("JSON"/"BIN\0").
const glbChunkHeaderSize = 8

func validateGLBContent(content []byte) error {
	if len(content) < glbHeaderSize+glbChunkHeaderSize {
		return ErrInvalidVisualAssetContent
	}
	if string(content[0:4]) != "glTF" {
		return ErrInvalidVisualAssetContent
	}
	version := binary.LittleEndian.Uint32(content[4:8])
	if version != 2 {
		return ErrInvalidVisualAssetContent
	}
	declaredLength := binary.LittleEndian.Uint32(content[8:12])
	if int(declaredLength) != len(content) {
		return ErrInvalidVisualAssetContent
	}

	jsonChunkLength := binary.LittleEndian.Uint32(content[12:16])
	jsonChunkType := string(content[16:20])
	if jsonChunkType != "JSON" {
		return ErrInvalidVisualAssetContent
	}
	jsonStart := glbHeaderSize + glbChunkHeaderSize
	jsonEnd := jsonStart + int(jsonChunkLength)
	if jsonEnd > len(content) {
		return ErrInvalidVisualAssetContent
	}

	var doc struct {
		Buffers []struct {
			URI string `json:"uri"`
		} `json:"buffers"`
		Images []struct {
			URI        string `json:"uri"`
			BufferView *int   `json:"bufferView"`
		} `json:"images"`
	}
	if err := json.Unmarshal(content[jsonStart:jsonEnd], &doc); err != nil {
		return ErrInvalidVisualAssetContent
	}
	for _, b := range doc.Buffers {
		if b.URI != "" {
			return ErrInvalidVisualAssetContent
		}
	}
	for _, img := range doc.Images {
		if img.URI != "" || img.BufferView == nil {
			return ErrInvalidVisualAssetContent
		}
	}
	return nil
}

// validateUSDZContent uses Go's standard archive/zip reader — far more
// robust than hand-parsing the ZIP central directory — and confirms every
// entry uses STORED (uncompressed) compression per the USDZ spec, which
// zip.Reader exposes via each File's Method field without this function
// needing to touch the raw byte layout.
func validateUSDZContent(content []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return ErrInvalidVisualAssetContent
	}
	if len(reader.File) == 0 {
		return ErrInvalidVisualAssetContent
	}
	for _, f := range reader.File {
		if f.Method != zip.Store {
			return ErrInvalidVisualAssetContent
		}
	}
	return nil
}
