package spatial

import (
	"bytes"
	"errors"
	"io"
	"math"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

var ErrGeneratedGeometryInvalid = errors.New("spatial: generated geometry failed validation")

// Configurable thresholds — deliberately package-level vars, not
// hardcoded literals inline, so the worker/composition root can override
// them without editing this file (RP4E0 spec §10: "configurable
// thresholds").
var (
	minGeneratedTriangles = 4
	// maxGeneratedTriangles was revised 2026-09-08 from an unverified
	// provisional 200,000 to 600,000 after live verification against the
	// real private renovex-hunyuan3d-runtime Space: a genuine, otherwise
	// fully valid generation produced 492,782 triangles for a single
	// real-world object. This is empirical provider-output-compatibility
	// tuning, not a Web-renderer performance target — a future slice may
	// still need mesh decimation/LODs before rendering something this
	// dense, which is explicitly not addressed here.
	maxGeneratedTriangles         = 600_000
	maxGeneratedBoundsMeters      = 50.0 // absolute sanity ceiling on any bounding-box dimension
	minGeneratedExtentMeters      = 1e-4 // EVERY axis (dx,dy,dz) must independently exceed this — rejects flat planes/lines, not just single points
	maxAspectRatio                = 100.0
	degenerateTriangleAreaEpsilon = 1e-9
	maxDegenerateTriangleFraction = 0.05
	// Pathological-fragmentation thresholds (spec §10, corrected — this
	// is an ENFORCED check, not logging-only): a component is
	// "pathologically tiny" if its triangle count is below
	// minComponentTriangleFraction of the mesh's total triangle count.
	// Validation fails only if the count of such tiny components exceeds
	// maxPathologicalComponents — this specifically targets shattered/
	// noise output (many tiny stray fragments), never a small number of
	// substantially-sized legitimate parts (a table + separate legs).
	minComponentTriangleFraction = 0.01
	maxPathologicalComponents    = 12
	// maxGeneratedBytes bounds every raw-bytes read this file/its callers
	// perform via io.LimitReader before io.ReadAll — never an unbounded
	// read of provider/staging-store output.
	maxGeneratedBytes int64 = 100 * 1024 * 1024
)

// readBounded reads at most maxGeneratedBytes+1 bytes from r via
// io.LimitReader before io.ReadAll, so a malicious/broken provider
// response or a corrupted staging object can never exhaust memory. If
// the limit is hit, the extra byte read distinguishes "exactly at the
// limit" from "over the limit" without needing a second read.
func readBounded(r io.Reader) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, maxGeneratedBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxGeneratedBytes {
		return nil, ErrGeneratedGeometryInvalid
	}
	return buf, nil
}

// validateGeneratedGeometry inspects a generated GLB's actual mesh
// geometry (RP4E0 spec §10). Runs AFTER RP4D's existing
// validateVisualAssetContent (structural check) and BEFORE
// PublishVisualAssetVersion. qmuntal/gltf types never leak past this
// file — the public signature is content []byte -> error only.
func validateGeneratedGeometry(content []byte) error {
	_, err := validateGeneratedGeometryWithDetail(content)
	return err
}

// generatedGeometryDetail carries the connected-component count for
// logging by the worker — informational only past the pathological-
// fragmentation check itself, which already ran.
type generatedGeometryDetail struct {
	ConnectedComponents int
}

func validateGeneratedGeometryWithDetail(content []byte) (generatedGeometryDetail, error) {
	if int64(len(content)) > maxGeneratedBytes {
		return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
	}
	doc := &gltf.Document{}
	if err := gltf.NewDecoder(bytes.NewReader(content)).Decode(doc); err != nil {
		return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
	}
	if len(doc.Meshes) == 0 {
		return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
	}

	allPositions := make([][3]float32, 0)
	allIndices := make([]uint32, 0)
	indexBase := uint32(0)
	for _, mesh := range doc.Meshes {
		for _, prim := range mesh.Primitives {
			if prim.Mode != gltf.PrimitiveTriangles {
				continue
			}
			posIdx, ok := prim.Attributes[gltf.POSITION]
			if !ok {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			if posIdx < 0 || posIdx >= len(doc.Accessors) {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			positions, err := modeler.ReadPosition(doc, doc.Accessors[posIdx], nil)
			if err != nil {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			if prim.Indices == nil {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			indicesAccessorIdx := *prim.Indices
			if indicesAccessorIdx < 0 || indicesAccessorIdx >= len(doc.Accessors) {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			indices, err := modeler.ReadIndices(doc, doc.Accessors[indicesAccessorIdx], nil)
			if err != nil {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			// Divisibility and bounds are validated BEFORE any index is
			// used to look into `positions` — a truncated index buffer
			// or an out-of-range index must be rejected cleanly, never
			// cause an out-of-bounds panic.
			if len(indices)%3 != 0 {
				return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
			}
			for _, idx := range indices {
				if int(idx) >= len(positions) {
					return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
				}
			}
			for _, idx := range indices {
				allIndices = append(allIndices, idx+indexBase)
			}
			allPositions = append(allPositions, positions...)
			indexBase += uint32(len(positions))
		}
	}
	if len(allPositions) == 0 || len(allIndices) == 0 {
		return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
	}
	// Re-validate divisibility/bounds on the FLATTENED allIndices/
	// allPositions too — the per-primitive checks above already
	// guarantee this given correct indexBase accumulation, but this is
	// the same invariant enforced once more, cheaply, immediately before
	// any code indexes into allPositions below.
	if len(allIndices)%3 != 0 {
		return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
	}
	for _, idx := range allIndices {
		if int(idx) >= len(allPositions) {
			return generatedGeometryDetail{}, ErrGeneratedGeometryInvalid
		}
	}

	if err := validateFiniteAndBounds(allPositions); err != nil {
		return generatedGeometryDetail{}, err
	}
	if err := validateTriangleCountAndDegenerate(allPositions, allIndices); err != nil {
		return generatedGeometryDetail{}, err
	}
	componentCount, err := validateFragmentation(allPositions, allIndices)
	if err != nil {
		return generatedGeometryDetail{}, err
	}
	return generatedGeometryDetail{ConnectedComponents: componentCount}, nil
}

// validateFiniteAndBounds requires GENUINE 3D extent: dx, dy, AND dz
// must EACH independently exceed minGeneratedExtentMeters — a flat plane
// (one axis ~zero, the other two non-zero) or a line (two axes ~zero) is
// rejected, not just the all-three-zero single-point case.
func validateFiniteAndBounds(positions [][3]float32) error {
	min := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	max := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for _, p := range positions {
		for i := 0; i < 3; i++ {
			v := p[i]
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return ErrGeneratedGeometryInvalid
			}
			if v < min[i] {
				min[i] = v
			}
			if v > max[i] {
				max[i] = v
			}
		}
	}
	dx, dy, dz := float64(max[0]-min[0]), float64(max[1]-min[1]), float64(max[2]-min[2])
	if dx < minGeneratedExtentMeters || dy < minGeneratedExtentMeters || dz < minGeneratedExtentMeters {
		return ErrGeneratedGeometryInvalid // degenerate — collapsed on at least one axis (point OR flat plane OR line)
	}
	maxDim := math.Max(dx, math.Max(dy, dz))
	if maxDim > maxGeneratedBoundsMeters {
		return ErrGeneratedGeometryInvalid
	}
	minDim := math.Min(dx, math.Min(dy, dz))
	if maxDim/minDim > maxAspectRatio {
		return ErrGeneratedGeometryInvalid
	}
	return nil
}

func validateTriangleCountAndDegenerate(positions [][3]float32, indices []uint32) error {
	triangleCount := len(indices) / 3
	if triangleCount < minGeneratedTriangles || triangleCount > maxGeneratedTriangles {
		return ErrGeneratedGeometryInvalid
	}
	degenerate := 0
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := positions[indices[i]], positions[indices[i+1]], positions[indices[i+2]]
		if triangleArea(a, b, c) < degenerateTriangleAreaEpsilon {
			degenerate++
		}
	}
	if float64(degenerate)/float64(triangleCount) > maxDegenerateTriangleFraction {
		return ErrGeneratedGeometryInvalid
	}
	return nil
}

func triangleArea(a, b, c [3]float32) float64 {
	ux, uy, uz := float64(b[0]-a[0]), float64(b[1]-a[1]), float64(b[2]-a[2])
	vx, vy, vz := float64(c[0]-a[0]), float64(c[1]-a[1]), float64(c[2]-a[2])
	cx, cy, cz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	return 0.5 * math.Sqrt(cx*cx+cy*cy+cz*cz)
}

// validateFragmentation is the pathological-fragmentation check —
// actually enforced, not merely logged. Computes each connected
// component's triangle count via union-find over shared vertices; a
// component contributing less than minComponentTriangleFraction of the
// mesh's total triangles is "pathologically tiny." If the COUNT of such
// tiny components exceeds maxPathologicalComponents, validation fails.
// Returns the total component count (for logging) alongside nil on
// success.
func validateFragmentation(positions [][3]float32, indices []uint32) (int, error) {
	parent := make([]int, len(positions))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	triangleCount := len(indices) / 3
	componentTriangles := make(map[int]int)
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := int(indices[i]), int(indices[i+1]), int(indices[i+2])
		union(a, b)
		union(b, c)
	}
	for i := 0; i+2 < len(indices); i += 3 {
		root := find(int(indices[i]))
		componentTriangles[root]++
	}

	pathological := 0
	for _, count := range componentTriangles {
		if float64(count)/float64(triangleCount) < minComponentTriangleFraction {
			pathological++
		}
	}
	if pathological > maxPathologicalComponents {
		return len(componentTriangles), ErrGeneratedGeometryInvalid
	}
	return len(componentTriangles), nil
}
