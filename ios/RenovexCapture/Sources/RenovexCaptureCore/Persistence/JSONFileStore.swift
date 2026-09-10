import Foundation

/// A minimal actor-isolated JSON-file key/value store — the shared
/// durability primitive `FileSpatialCaptureRepository` and
/// `FileRoomDraftRepository` build on (plan §RP3: local persistence must
/// survive app restart/process-memory loss, proven deterministically on
/// Windows via `swift test` rather than only claimed).
///
/// One JSON file per key, under `directoryURL`. Deliberately not a single
/// combined JSON blob: two runs for the same Space stay physically distinct
/// files, which is what makes "delete/discard one unconfirmed scan ->
/// unrelated runs remain" trivially true rather than something a shared-file
/// writer could accidentally violate.
actor JSONFileStore {
    private let directoryURL: URL
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    init(directoryURL: URL) throws {
        self.directoryURL = directoryURL
        self.encoder = JSONEncoder()
        // .secondsSince1970 (not .iso8601): ISO8601DateFormatter's default
        // string has whole-second precision, silently truncating Date()'s
        // sub-second component on every encode/decode round trip — this is
        // an internal file format, not a wire format needing ISO8601
        // readability, so exact round-trip fidelity wins over
        // human-readability.
        self.encoder.dateEncodingStrategy = .secondsSince1970
        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .secondsSince1970
        try FileManager.default.createDirectory(at: directoryURL, withIntermediateDirectories: true)
    }

    private func fileURL(forKey key: String) -> URL {
        // Keys are repository-generated UUID strings in every current
        // caller, never raw user input — no path-traversal sanitization
        // needed, but percent-encode defensively so an unexpected key
        // containing "/" cannot escape directoryURL.
        let safeKey = key.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? key
        return directoryURL.appendingPathComponent(safeKey).appendingPathExtension("json")
    }

    func write<T: Encodable>(_ value: T, forKey key: String) throws {
        let data = try encoder.encode(value)
        try data.write(to: fileURL(forKey: key), options: .atomic)
    }

    func read<T: Decodable>(_ type: T.Type, forKey key: String) throws -> T? {
        let url = fileURL(forKey: key)
        guard FileManager.default.fileExists(atPath: url.path) else { return nil }
        let data = try Data(contentsOf: url)
        return try decoder.decode(T.self, from: data)
    }

    func readAll<T: Decodable>(_ type: T.Type) throws -> [T] {
        let contents = try FileManager.default.contentsOfDirectory(at: directoryURL, includingPropertiesForKeys: nil)
        return try contents
            .filter { $0.pathExtension == "json" }
            .map { try decoder.decode(T.self, from: Data(contentsOf: $0)) }
    }
}
