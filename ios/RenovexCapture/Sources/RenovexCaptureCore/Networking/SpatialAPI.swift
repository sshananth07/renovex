import Foundation

/// Mirrors `backend/internal/spatial/handler.go`'s JSON contracts exactly
/// (plan §RP3.5/§RP4B0) — the capture/artifact sync substrate this task
/// builds. Only the fields the sync layer needs are modeled; this is not a
/// full spatial admin client.

// MARK: - Capture

public struct SpatialCaptureDTO: Codable, Sendable, Identifiable {
    public let id: String
    public let projectID: String
    public let spaceID: String
    public let status: String
    public let roomVersionID: String?
    public let provider: String
    public let captureNumber: Int
    public let roomDraftID: String?
    public let clientCaptureID: String?
    public let createdAt: String
    public let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case projectID = "projectId"
        case spaceID = "spaceId"
        case status
        case roomVersionID = "roomVersionId"
        case provider
        case captureNumber
        case roomDraftID = "roomDraftId"
        case clientCaptureID = "clientCaptureId"
        case createdAt
        case updatedAt
    }
}

public struct StartCaptureRequestBody: Encodable, Sendable {
    public let projectID: String
    public let spaceID: String
    public let provider: String?
    public let clientCaptureID: String?

    enum CodingKeys: String, CodingKey {
        case projectID = "projectId"
        case spaceID = "spaceId"
        case provider
        case clientCaptureID = "clientCaptureId"
    }

    public init(projectID: String, spaceID: String, provider: String? = nil, clientCaptureID: String? = nil) {
        self.projectID = projectID
        self.spaceID = spaceID
        self.provider = provider
        self.clientCaptureID = clientCaptureID
    }
}

// MARK: - Artifact

public struct SpatialArtifactDTO: Codable, Sendable, Identifiable {
    public let id: String
    public let captureID: String
    public let kind: String
    public let contentType: String
    public let declaredSize: Int64
    public let actualSize: Int64?
    public let status: String
    public let createdAt: String
    public let uploadedAt: String?

    enum CodingKeys: String, CodingKey {
        case id
        case captureID = "captureId"
        case kind
        case contentType
        case declaredSize
        case actualSize
        case status
        case createdAt
        case uploadedAt
    }
}

public struct RequestArtifactUploadRequestBody: Encodable, Sendable {
    public let captureID: String
    public let kind: String
    public let contentType: String
    public let declaredSize: Int64
    public let checksum: String

    enum CodingKeys: String, CodingKey {
        case captureID = "captureId"
        case kind
        case contentType
        case declaredSize
        case checksum
    }

    public init(captureID: String, kind: String, contentType: String, declaredSize: Int64, checksum: String) {
        self.captureID = captureID
        self.kind = kind
        self.contentType = contentType
        self.declaredSize = declaredSize
        self.checksum = checksum
    }
}

public struct ArtifactUploadResponseBody: Decodable, Sendable {
    public let artifact: SpatialArtifactDTO
    public let uploadToken: String
}

public struct FinalizeArtifactUploadRequestBody: Encodable, Sendable {
    public let uploadToken: String

    public init(uploadToken: String) {
        self.uploadToken = uploadToken
    }
}
