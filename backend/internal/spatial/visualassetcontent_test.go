package spatial

import (
	"errors"
	"testing"
)

func buildTestGLB(t *testing.T, jsonChunk string, includeBinChunk bool) []byte {
	t.Helper()
	pad := func(b []byte, fill byte) []byte {
		for len(b)%4 != 0 {
			b = append(b, fill)
		}
		return b
	}
	json := pad([]byte(jsonChunk), ' ')

	glb := []byte{}
	appendU32 := func(v uint32) {
		glb = append(glb, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}

	// Header: magic, version, total length placeholder (patched at the end).
	glb = append(glb, 'g', 'l', 'T', 'F')
	appendU32(2)
	appendU32(0) // patched below

	// JSON chunk.
	appendU32(uint32(len(json)))
	glb = append(glb, 'J', 'S', 'O', 'N')
	glb = append(glb, json...)

	if includeBinChunk {
		bin := pad([]byte{1, 2, 3, 4}, 0)
		appendU32(uint32(len(bin)))
		glb = append(glb, 'B', 'I', 'N', 0)
		glb = append(glb, bin...)
	}

	total := uint32(len(glb))
	glb[8] = byte(total)
	glb[9] = byte(total >> 8)
	glb[10] = byte(total >> 16)
	glb[11] = byte(total >> 24)
	return glb
}

func TestValidateVisualAssetContent_AcceptsSelfContainedGLB(t *testing.T) {
	glb := buildTestGLB(t, `{"asset":{"version":"2.0"},"buffers":[{"byteLength":4}]}`, true)
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsGLBWithExternalBufferURI(t *testing.T) {
	glb := buildTestGLB(t, `{"asset":{"version":"2.0"},"buffers":[{"uri":"external.bin","byteLength":4}]}`, false)
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsGLBWithExternalImageURI(t *testing.T) {
	glb := buildTestGLB(t, `{"asset":{"version":"2.0"},"images":[{"uri":"texture.png"}]}`, false)
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_AcceptsGLBWithBufferViewImage(t *testing.T) {
	glb := buildTestGLB(t, `{"asset":{"version":"2.0"},"images":[{"bufferView":0,"mimeType":"image/png"}]}`, true)
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsEmptyBytes(t *testing.T) {
	if err := validateVisualAssetContent(VisualAssetFormatGLB, nil); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsWrongMagic(t *testing.T) {
	bad := []byte("NOTGLTF!" + string(make([]byte, 20)))
	if err := validateVisualAssetContent(VisualAssetFormatGLB, bad); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsTruncatedGLB(t *testing.T) {
	if err := validateVisualAssetContent(VisualAssetFormatGLB, []byte("glTF")); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsDeclaredLengthMismatch(t *testing.T) {
	glb := buildTestGLB(t, `{"asset":{"version":"2.0"}}`, false)
	glb[8] = 255 // corrupt the declared total length
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsMalformedJSONChunk(t *testing.T) {
	glb := buildTestGLB(t, `{not valid json`, false)
	if err := validateVisualAssetContent(VisualAssetFormatGLB, glb); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

// --- USDZ ---

func buildTestUSDZ(t *testing.T, stored bool) []byte {
	t.Helper()
	// A minimal single-entry ZIP archive, built by hand (not via
	// archive/zip's Writer, which defaults to deflate) so the test can
	// control the compression method byte directly.
	content := []byte("USDA text content")
	method := uint16(0) // stored
	if !stored {
		method = 8 // deflate
	}
	var buf []byte
	appendU16 := func(v uint16) { buf = append(buf, byte(v), byte(v>>8)) }
	appendU32 := func(v uint32) {
		buf = append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	name := "model.usda"

	localHeaderOffset := uint32(0)
	// Local file header.
	appendU32(0x04034b50)
	appendU16(20) // version needed
	appendU16(0)  // flags
	appendU16(method)
	appendU16(0) // mod time
	appendU16(0) // mod date
	appendU32(0) // crc32 (not validated by this test)
	appendU32(uint32(len(content)))
	appendU32(uint32(len(content)))
	appendU16(uint16(len(name)))
	appendU16(0) // extra field length
	buf = append(buf, name...)
	buf = append(buf, content...)

	centralDirOffset := uint32(len(buf))
	// Central directory header.
	appendU32(0x02014b50)
	appendU16(20) // version made by
	appendU16(20) // version needed
	appendU16(0)  // flags
	appendU16(method)
	appendU16(0) // mod time
	appendU16(0) // mod date
	appendU32(0) // crc32
	appendU32(uint32(len(content)))
	appendU32(uint32(len(content)))
	appendU16(uint16(len(name)))
	appendU16(0) // extra field length
	appendU16(0) // comment length
	appendU16(0) // disk number start
	appendU16(0) // internal attrs
	appendU32(0) // external attrs
	appendU32(localHeaderOffset)
	buf = append(buf, name...)

	centralDirSize := uint32(len(buf)) - centralDirOffset
	// End of central directory record.
	appendU32(0x06054b50)
	appendU16(0) // disk number
	appendU16(0) // disk with central dir
	appendU16(1) // entries on this disk
	appendU16(1) // total entries
	appendU32(centralDirSize)
	appendU32(centralDirOffset)
	appendU16(0) // comment length

	return buf
}

func TestValidateVisualAssetContent_AcceptsStoredUSDZ(t *testing.T) {
	usdz := buildTestUSDZ(t, true)
	if err := validateVisualAssetContent(VisualAssetFormatUSDZ, usdz); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsDeflatedUSDZ(t *testing.T) {
	usdz := buildTestUSDZ(t, false)
	if err := validateVisualAssetContent(VisualAssetFormatUSDZ, usdz); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}

func TestValidateVisualAssetContent_RejectsMalformedZIP(t *testing.T) {
	if err := validateVisualAssetContent(VisualAssetFormatUSDZ, []byte("not a zip file at all")); !errors.Is(err, ErrInvalidVisualAssetContent) {
		t.Fatalf("expected ErrInvalidVisualAssetContent, got %v", err)
	}
}
