package spatial

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// buildTestGeometryGLB constructs a minimal, valid, SELF-CONTAINED BINARY GLB
// (via gltf.NewEncoder(...).Encode, which produces the real .glb binary
// container — never a bare glTF-JSON-only document) from explicit
// positions/indices, using qmuntal/gltf's own writer — reusing the
// library under test to build fixtures avoids hand-rolled binary bugs.
func buildTestGeometryGLB(t *testing.T, positions [][3]float32, indices []uint32) []byte {
	t.Helper()
	doc := gltf.NewDocument()
	positionAccessor := modeler.WritePosition(doc, positions)
	indexAccessor := modeler.WriteIndices(doc, indices)
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Primitives: []*gltf.Primitive{{
			Indices:    gltf.Index(indexAccessor),
			Attributes: gltf.PrimitiveAttributes{gltf.POSITION: positionAccessor},
			Mode:       gltf.PrimitiveTriangles,
		}},
	})
	doc.Nodes = append(doc.Nodes, &gltf.Node{Mesh: gltf.Index(0)})
	doc.Scenes[0].Nodes = []int{0}

	var buf bytes.Buffer
	if err := gltf.NewEncoder(&buf).Encode(doc); err != nil {
		t.Fatalf("encoding test GLB: %v", err)
	}
	got := buf.Bytes()
	limit := 16
	if len(got) < limit {
		limit = len(got)
	}
	if len(got) < 4 || string(got[0:4]) != "glTF" {
		t.Fatalf("buildTestGeometryGLB did not produce a binary GLB container (missing 'glTF' magic) — got %d bytes starting %q", len(got), got[:limit])
	}
	return got
}

// unitBoxPositions/unitBoxIndices describe a simple triangulated unit
// cube: 8 vertices, 12 triangles (2 per face × 6 faces) — well above any
// reasonable minGeneratedTriangles default, and genuinely 3-dimensional
// (non-zero extent on all three axes), so this fixture is valid against
// EVERY check in this file simultaneously, not just triangle count.
func unitBoxPositions() [][3]float32 {
	return [][3]float32{
		{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}, // back face (z=0)
		{0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1}, // front face (z=1)
	}
}

func unitBoxIndices() []uint32 {
	return []uint32{
		0, 1, 2, 0, 2, 3, // back
		4, 6, 5, 4, 7, 6, // front
		0, 4, 5, 0, 5, 1, // bottom
		3, 2, 6, 3, 6, 7, // top
		0, 3, 7, 0, 7, 4, // left
		1, 5, 6, 1, 6, 2, // right
	}
}

func buildValidTestGLB(t *testing.T) []byte {
	t.Helper()
	return buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
}

func TestValidateGeneratedGeometry_ValidBoxAccepted(t *testing.T) {
	if err := validateGeneratedGeometry(buildValidTestGLB(t)); err != nil {
		t.Errorf("expected a valid 12-triangle box GLB to pass validation, got %v", err)
	}
}

func TestValidateGeneratedGeometry_NaNPositionRejected(t *testing.T) {
	// qmuntal/gltf's own JSON encoder rejects NaN outright (Go's
	// encoding/json never serializes NaN — this is genuine stdlib
	// behavior, not something this test can route through
	// buildTestGeometryGLB's normal writer). Build a valid GLB first,
	// then binary-patch ONE float32 in the raw binary buffer chunk to
	// the IEEE-754 NaN bit pattern directly, proving the validator
	// rejects NaN it encounters in real (if malformed) provider output —
	// a case a well-behaved encoder would never itself produce, but a
	// buggy/adversarial provider response legitimately could.
	content := buildTestGeometryGLB(t, unitBoxPositions(), unitBoxIndices())
	patched := make([]byte, len(content))
	copy(patched, content)
	nanBits := math.Float32bits(float32(math.NaN()))
	// The first position accessor's binary data starts immediately after
	// the JSON chunk header+body within the BIN chunk; rather than
	// hand-parsing chunk offsets, scan for the first vertex's known
	// bytes (unitBoxPositions()[0] = {0,0,0}, all-zero — distinguishing
	// bytes come from vertex 1 = {1,0,0}) and overwrite that float32's
	// 4 bytes with the NaN bit pattern.
	target := math.Float32bits(1) // vertex 1's X component, 1.0
	targetBytes := []byte{byte(target), byte(target >> 8), byte(target >> 16), byte(target >> 24)}
	found := false
	for i := 0; i+4 <= len(patched); i++ {
		if patched[i] == targetBytes[0] && patched[i+1] == targetBytes[1] && patched[i+2] == targetBytes[2] && patched[i+3] == targetBytes[3] {
			patched[i] = byte(nanBits)
			patched[i+1] = byte(nanBits >> 8)
			patched[i+2] = byte(nanBits >> 16)
			patched[i+3] = byte(nanBits >> 24)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("test setup failure: could not locate a known float32 value in the encoded GLB to patch to NaN")
	}
	if err := validateGeneratedGeometry(patched); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for a NaN position, got %v", err)
	}
}

func TestValidateGeneratedGeometry_DegenerateSinglePointRejected(t *testing.T) {
	// Enough triangles to clear the triangle-count floor, but every
	// vertex is the SAME point — dx=dy=dz=0.
	positions := make([][3]float32, len(unitBoxPositions()))
	for i := range positions {
		positions[i] = [3]float32{1, 1, 1}
	}
	content := buildTestGeometryGLB(t, positions, unitBoxIndices())
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for a degenerate single-point mesh, got %v", err)
	}
}

func TestValidateGeneratedGeometry_FlatPlaneRejected(t *testing.T) {
	// CORRECTION-SPECIFIC TEST: dx and dy are non-zero, dz is exactly
	// zero for every vertex — a flat plane. The PREVIOUS (buggy)
	// implementation only rejected dx<=0 && dy<=0 && dz<=0 (all three
	// collapsed), so this case would have been WRONGLY ACCEPTED before
	// this correction. Proves the fix.
	positions := unitBoxPositions()
	for i := range positions {
		positions[i][2] = 0 // flatten every vertex onto z=0
	}
	content := buildTestGeometryGLB(t, positions, unitBoxIndices())
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for a flat (zero-thickness) mesh, got %v", err)
	}
}

func TestValidateGeneratedGeometry_TooFewTrianglesRejected(t *testing.T) {
	old := minGeneratedTriangles
	minGeneratedTriangles = 20 // above the box fixture's 12 triangles
	defer func() { minGeneratedTriangles = old }()
	content := buildValidTestGLB(t)
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for too few triangles, got %v", err)
	}
}

// buildTriangleGridGLB constructs a single-component, well-bounded,
// non-degenerate triangulated grid mesh with (gridSize-1)^2*2 triangles —
// used to exercise maxGeneratedTriangles' boundary with a realistically
// shaped mesh (not a degenerate/fragmented one), staying within
// maxGeneratedBoundsMeters/maxAspectRatio regardless of size by scaling
// the step so total span stays fixed at 1.0 x 1.0 x a small z-lift.
func buildTriangleGridGLB(t *testing.T, gridSize int) []byte {
	t.Helper()
	step := float32(1.0) / float32(gridSize-1)
	var positions [][3]float32
	for y := 0; y < gridSize; y++ {
		for x := 0; x < gridSize; x++ {
			positions = append(positions, [3]float32{float32(x) * step, float32(y) * step, 0})
		}
	}
	// Genuine 3D extent, matching the fragmentation fixture's convention.
	positions[len(positions)-1][2] = 0.05

	var indices []uint32
	for y := 0; y < gridSize-1; y++ {
		for x := 0; x < gridSize-1; x++ {
			i0 := uint32(y*gridSize + x)
			i1 := i0 + 1
			i2 := i0 + uint32(gridSize)
			i3 := i2 + 1
			indices = append(indices, i0, i1, i2, i1, i3, i2)
		}
	}
	return buildTestGeometryGLB(t, positions, indices)
}

// TestValidateGeneratedGeometry_RealHunyuanTriangleCountAccepted pins the
// exact triangle count observed live 2026-09-08 against the real private
// renovex-hunyuan3d-runtime Space (492,782 triangles, otherwise fully
// valid) — proving the revised 600,000 ceiling (raised from an unverified
// provisional 200,000) genuinely accepts real provider output, not just a
// synthetic boundary case.
func TestValidateGeneratedGeometry_RealHunyuanTriangleCountAccepted(t *testing.T) {
	// gridSize chosen so (gridSize-1)^2*2 is close to, but not exactly,
	// the observed 492,782 — the point is "a realistic mesh at roughly
	// this density passes," not reproducing the exact number.
	const gridSize = 497 // (497-1)^2*2 = 492,032 triangles
	content := buildTriangleGridGLB(t, gridSize)
	if err := validateGeneratedGeometry(content); err != nil {
		t.Errorf("expected a mesh at the real observed Hunyuan3D triangle density (~492k) to pass, got %v", err)
	}
}

// TestValidateGeneratedGeometry_JustBelowRevisedCeilingAccepted proves
// the revised 600,000 ceiling itself, not just the empirical ~492k case.
func TestValidateGeneratedGeometry_JustBelowRevisedCeilingAccepted(t *testing.T) {
	const gridSize = 548 // (548-1)^2*2 = 598,258 triangles, just under 600,000
	content := buildTriangleGridGLB(t, gridSize)
	if err := validateGeneratedGeometry(content); err != nil {
		t.Errorf("expected a mesh just under the 600,000 ceiling to pass, got %v", err)
	}
}

// TestValidateGeneratedGeometry_AboveRevisedCeilingRejected proves the
// raised ceiling still rejects genuinely excessive triangle counts.
func TestValidateGeneratedGeometry_AboveRevisedCeilingRejected(t *testing.T) {
	const gridSize = 600 // (600-1)^2*2 = 718,802 triangles, over 600,000
	content := buildTriangleGridGLB(t, gridSize)
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid above the 600,000 ceiling, got %v", err)
	}
}

func TestValidateGeneratedGeometry_ExcessiveBoundsRejected(t *testing.T) {
	positions := unitBoxPositions()
	for i := range positions {
		positions[i][0] *= 1000
		positions[i][1] *= 1000
		positions[i][2] *= 1000
	}
	content := buildTestGeometryGLB(t, positions, unitBoxIndices())
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for absurdly large bounds, got %v", err)
	}
}

func TestValidateGeneratedGeometry_OutOfRangeIndexRejectedNotPanicking(t *testing.T) {
	// CORRECTION-SPECIFIC TEST: an index pointing past the end of the
	// positions accessor must be rejected cleanly, not panic.
	positions := unitBoxPositions() // 8 positions, valid indices are 0-7
	indices := unitBoxIndices()
	indices[0] = 999 // out of range
	content := buildTestGeometryGLB(t, positions, indices)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("validateGeneratedGeometry panicked on an out-of-range index instead of returning an error: %v", r)
		}
	}()
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for an out-of-range index, got %v", err)
	}
}

func TestValidateGeneratedGeometry_NonDivisibleByThreeIndexCountRejected(t *testing.T) {
	// CORRECTION-SPECIFIC TEST: a truncated index buffer (not a multiple
	// of 3) must be rejected outright, never silently truncate the last
	// partial triangle.
	positions := unitBoxPositions()
	indices := unitBoxIndices()
	indices = indices[:len(indices)-1] // drop one index, breaking %3==0
	content := buildTestGeometryGLB(t, positions, indices)
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for a non-multiple-of-3 index count, got %v", err)
	}
}

func TestValidateGeneratedGeometry_LegitimateMultiComponentMeshAccepted(t *testing.T) {
	// Two substantially-sized components (e.g. a table leg + a separate
	// top) — must NOT be rejected merely for having more than one
	// connected component (RP4E0 spec §10, corrected: an ENFORCED
	// pathological-fragmentation threshold, not a blanket count>1
	// rejection). Two boxes, each well above the tiny-fraction threshold.
	basePositions := unitBoxPositions()
	baseIndices := unitBoxIndices()
	offsetPositions := make([][3]float32, len(basePositions))
	for i, p := range basePositions {
		offsetPositions[i] = [3]float32{p[0] + 10, p[1] + 10, p[2] + 10}
	}
	offsetIndices := make([]uint32, len(baseIndices))
	for i, idx := range baseIndices {
		offsetIndices[i] = idx + uint32(len(basePositions))
	}
	allPositions := append(append([][3]float32{}, basePositions...), offsetPositions...)
	allIndices := append(append([]uint32{}, baseIndices...), offsetIndices...)

	content := buildTestGeometryGLB(t, allPositions, allIndices)
	if err := validateGeneratedGeometry(content); err != nil {
		t.Errorf("expected two substantially-sized components to pass, got %v", err)
	}
}

func TestValidateGeneratedGeometry_ManyTinyFragmentsRejected(t *testing.T) {
	// CORRECTION-SPECIFIC TEST: many tiny single-triangle "islands"
	// alongside one dominant, densely-triangulated component — the
	// PATHOLOGICAL-FRAGMENTATION case the spec explicitly wants ENFORCED
	// (not just logged). The dominant component uses a fine grid (many
	// triangles, all within maxGeneratedBoundsMeters) so each stray
	// triangle's fraction of the total genuinely falls below
	// minComponentTriangleFraction — a small, widely-separated dominant
	// shape (as an earlier draft of this test used) would trip the
	// bounds/aspect-ratio check first and never actually exercise
	// fragmentation at all, which this version deliberately avoids.
	var positions [][3]float32
	var indices []uint32
	const gridSize = 21   // 21x21 vertices -> 20x20 quads -> 800 triangles
	const gridStep = 0.05 // total grid span: 1.0 x 1.0 units
	for y := 0; y < gridSize; y++ {
		for x := 0; x < gridSize; x++ {
			positions = append(positions, [3]float32{float32(x) * gridStep, float32(y) * gridStep, 0})
		}
	}
	for y := 0; y < gridSize-1; y++ {
		for x := 0; x < gridSize-1; x++ {
			i0 := uint32(y*gridSize + x)
			i1 := i0 + 1
			i2 := i0 + uint32(gridSize)
			i3 := i2 + 1
			indices = append(indices, i0, i1, i2, i1, i3, i2)
		}
	}
	// Give the grid genuine 3D extent (it's currently flat, z=0 for
	// every vertex, which validateFiniteAndBounds would reject on its
	// own) by lifting one corner — chosen so the resulting z-extent
	// (0.05) keeps the max/min aspect ratio (1.0 / 0.05 = 20) well under
	// maxAspectRatio (100), unlike an earlier draft of this fixture that
	// used a near-zero z-lift and tripped the aspect-ratio check instead
	// of ever reaching the fragmentation check this test exists to prove.
	const zLift = float32(0.05)
	positions[len(positions)-1][2] = zLift

	for i := 0; i < maxPathologicalComponents+5; i++ {
		base := uint32(len(positions))
		off := float32(0.2) + float32(i)*0.03 // within the grid's own bounds, far enough apart (index-wise) to stay disjoint components
		positions = append(positions, [3]float32{off, off, zLift}, [3]float32{off + 0.001, off, zLift}, [3]float32{off, off + 0.001, zLift})
		indices = append(indices, base, base+1, base+2)
	}
	content := buildTestGeometryGLB(t, positions, indices)
	if err := validateGeneratedGeometry(content); !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for pathological fragmentation (many tiny stray islands), got %v", err)
	}
}

func TestValidateFragmentation_TwoSubstantialComponentsPass(t *testing.T) {
	positions := [][3]float32{
		{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1},
		{10, 10, 10}, {11, 10, 10}, {10, 11, 10}, {10, 10, 11},
	}
	// 4 triangles per component, well above the tiny-fraction threshold for a 8-triangle total.
	indices := []uint32{
		0, 1, 2, 0, 1, 3, 0, 2, 3, 1, 2, 3,
		4, 5, 6, 4, 5, 7, 4, 6, 7, 5, 6, 7,
	}
	count, err := validateFragmentation(positions, indices)
	if err != nil {
		t.Errorf("expected two substantial components to pass, got %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 components reported, got %d", count)
	}
}

func TestValidateFragmentation_ManyTinyFragmentsFail(t *testing.T) {
	// Dominant component: a 20x20 grid of connected triangles (800
	// triangles, all sharing vertices with their neighbors so union-find
	// treats it as ONE component) — large enough that each 1-triangle
	// stray fragment below is genuinely under minComponentTriangleFraction
	// (1%) of the total triangle count.
	var positions [][3]float32
	var indices []uint32
	const gridSize = 21 // 21x21 vertices -> 20x20 quads -> 800 triangles
	for y := 0; y < gridSize; y++ {
		for x := 0; x < gridSize; x++ {
			positions = append(positions, [3]float32{float32(x), float32(y), 0})
		}
	}
	for y := 0; y < gridSize-1; y++ {
		for x := 0; x < gridSize-1; x++ {
			i0 := uint32(y*gridSize + x)
			i1 := i0 + 1
			i2 := i0 + uint32(gridSize)
			i3 := i2 + 1
			indices = append(indices, i0, i1, i2, i1, i3, i2)
		}
	}

	for i := 0; i < maxPathologicalComponents+3; i++ {
		base := uint32(len(positions))
		off := float32(1000 + i*5)
		positions = append(positions, [3]float32{off, off, off}, [3]float32{off + 0.01, off, off}, [3]float32{off, off + 0.01, off})
		indices = append(indices, base, base+1, base+2)
	}
	_, err := validateFragmentation(positions, indices)
	if !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected pathological fragmentation to be rejected, got %v", err)
	}
}

func TestReadBounded_RejectsOversizedStream(t *testing.T) {
	old := maxGeneratedBytes
	maxGeneratedBytes = 10
	defer func() { maxGeneratedBytes = old }()

	oversized := bytes.NewReader(make([]byte, 100))
	_, err := readBounded(oversized)
	if !errors.Is(err, ErrGeneratedGeometryInvalid) {
		t.Errorf("expected ErrGeneratedGeometryInvalid for a stream exceeding maxGeneratedBytes, got %v", err)
	}
}

func TestReadBounded_AcceptsStreamWithinLimit(t *testing.T) {
	old := maxGeneratedBytes
	maxGeneratedBytes = 100
	defer func() { maxGeneratedBytes = old }()

	within := bytes.NewReader(make([]byte, 50))
	buf, err := readBounded(within)
	if err != nil {
		t.Fatalf("readBounded: %v", err)
	}
	if len(buf) != 50 {
		t.Errorf("expected 50 bytes read, got %d", len(buf))
	}
}
