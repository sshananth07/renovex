// swift-tools-version: 5.9
import PackageDescription

// Renovex Capture — iOS RoomPlan-based spatial capture client (M8.5C RP1).
//
// Module boundary (design spec §8.7): RenovexCaptureCore is provider-neutral
// domain code with zero Apple/RoomPlan-specific types in any public
// signature. RenovexCaptureRoomPlan is the only target that imports
// RoomPlan/ARKit; it depends on Core and adapts Apple's capture output into
// Core's domain types. RenovexCaptureApp is the SwiftUI application shell
// and depends on both — mirroring the frozen Android provider's
// package-boundary discipline (capture/reconstruction pure-Kotlin vs.
// capture/arcore ARCore-specific), described in the design spec as the
// pattern to preserve across providers.
//
// Platform split (added when Windows Swift 6.3.3 verification began,
// 2026-09-03): RenovexCaptureRoomPlan and RenovexCaptureApp import
// RoomPlan/SwiftUI, which do not exist outside Apple platforms — this is a
// genuine Apple-framework boundary, not a module-boundary defect. `swift
// test` links one combined test binary for every test target in the
// package by default, so without this split, attempting to run the
// Windows-buildable RenovexCaptureCoreTests would always also try (and
// fail) to build RenovexCaptureRoomPlan/RenovexCaptureApp first. The
// targets/products below are conditionally included via `#if os(...)` so
// that on Windows/Linux only RenovexCaptureCore and RenovexCaptureCoreTests
// exist in the build graph, while on macOS/iOS the full graph (RoomPlan +
// App + their tests) remains included exactly as before. This is a build-
// graph-visibility change only — no Swift source semantics changed, no
// test was weakened or skipped, and the RenovexCaptureCore/RenovexCaptureRoomPlan/
// RenovexCaptureApp module boundary itself is unchanged.

// swift-crypto (Apple's official, cross-platform, pure-Swift crypto
// package — works on Linux/Windows/Apple, unlike CryptoKit which is
// Apple-only) provides SHA256 for the artifact-checksum contract (plan
// §RP3.5/§RP4B0): the canonical SHA-256 hex digest RequestArtifactUpload/
// FinalizeArtifactUpload expect must match exactly what
// backend/internal/platform/composition/spatialartifactstoreadapter.go
// computes server-side. Isolated behind ChecksumUtility rather than
// exposed throughout the domain layer.
let packageDependencies: [Package.Dependency] = [
    .package(url: "https://github.com/apple/swift-crypto.git", from: "3.0.0"),
]

var packageTargets: [Target] = [
    .target(
        name: "RenovexCaptureCore",
        dependencies: [.product(name: "Crypto", package: "swift-crypto")],
        path: "Sources/RenovexCaptureCore"
    ),
    .testTarget(
        name: "RenovexCaptureCoreTests",
        dependencies: ["RenovexCaptureCore"],
        path: "Tests/RenovexCaptureCoreTests"
    ),
]

var packageProducts: [Product] = [
    .library(name: "RenovexCaptureCore", targets: ["RenovexCaptureCore"]),
]

#if os(macOS) || os(iOS)
packageTargets.append(contentsOf: [
    .target(
        name: "RenovexCaptureRoomPlan",
        dependencies: ["RenovexCaptureCore"],
        path: "Sources/RenovexCaptureRoomPlan"
    ),
    .target(
        name: "RenovexCaptureApp",
        dependencies: ["RenovexCaptureCore", "RenovexCaptureRoomPlan"],
        path: "Sources/RenovexCaptureApp"
    ),
    .testTarget(
        name: "RenovexCaptureAppTests",
        dependencies: ["RenovexCaptureApp"],
        path: "Tests/RenovexCaptureAppTests"
    ),
])
packageProducts.append(contentsOf: [
    .library(name: "RenovexCaptureRoomPlan", targets: ["RenovexCaptureRoomPlan"]),
    .library(name: "RenovexCaptureApp", targets: ["RenovexCaptureApp"]),
])
#endif

let package = Package(
    name: "RenovexCapture",
    platforms: [
        .iOS(.v16) // RoomPlan requires iOS 16+; confirmed against Apple's
                   // current RoomPlan documentation at RP1 implementation
                   // time (2026-09-03). Re-verify if implementation resumes
                   // much later, since Apple's minimum can change. This
                   // `platforms` declaration only takes effect when
                   // building for an Apple platform (e.g. via Xcode); it
                   // has no bearing on the `#if os(...)` split above, which
                   // controls what SwiftPM even attempts to build on
                   // Windows/Linux.
    ],
    products: packageProducts,
    dependencies: packageDependencies,
    targets: packageTargets
)
