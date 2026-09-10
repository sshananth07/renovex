import Foundation

/// Mirrors `backend/internal/projects/handler.go` and
/// `backend/internal/spaces/handler.go`'s JSON contracts exactly (Task 1
/// inspection, reused from the frozen Android provider's
/// `network/ProjectSpaceApi.kt`). Only the fields the iOS project/space
/// SELECTION flow needs are modeled — this is a capture-companion client,
/// not a full project-management client.
public struct PaginatedResponseBody<Item: Decodable & Sendable>: Decodable, Sendable {
    public let items: [Item]
    public let page: Int
    public let pageSize: Int
    public let total: Int
}

public struct ProjectDTO: Decodable, Sendable, Identifiable {
    public let id: String
    public let clientID: String
    public let name: String
    public let status: String
    public let scopeBrief: String
    public let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case clientID = "clientId"
        case name
        case status
        case scopeBrief
        case createdAt
    }
}

public struct SpaceDTO: Decodable, Sendable, Identifiable {
    public let id: String
    public let projectID: String
    public let name: String
    public let type: String
    public let description: String
    public let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case projectID = "projectId"
        case name
        case type
        case description
        case createdAt
    }
}
