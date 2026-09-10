import Foundation
import RenovexCaptureCore

public enum LoginResult: Sendable, Equatable {
    case success
    case invalidCredentials 
    case networkUnavailable
    case timedOut 
    case serverError
    case unknownError
}

/// The app's top-level observable session state: auth + current
/// project/space selection. Mirrors the frozen Android provider's
/// `RenovexNavHost` composable's local state, but factored into an
/// `@Observable` type per SwiftUI convention rather than kept inline in a
/// view.
@MainActor
@Observable
public final class AppSession {
    public private(set) var authState: AuthSessionState = .unknown
    public private(set) var selection: ProjectSpaceSelection = .empty

    private let apiClient: RenovexAPIClient
    private let sessionStore: PersistedSessionFlagStore

    public init(
        apiClient: RenovexAPIClient,
        sessionStore: PersistedSessionFlagStore
    ) {
        self.apiClient = apiClient
        self.sessionStore = sessionStore
    }

    /// Called once at app launch (RP1 TDD requirement: "authenticated API
    /// state"). Mirrors the frozen Android provider's
    /// `AuthRepository.restoreSession()`: reads the persisted-session flag;
    /// if set, attempts a refresh using the persisted refresh-token cookie.
    public func restoreSession() async {
        let hasPersisted = await sessionStore.hasPersistedSession()

        let initial = AuthSessionReducer.onAppLaunch(
            hasPersistedSession: hasPersisted
        )

        guard case .unknown = initial else {
            authState = initial
            return
        }

        authState = await apiClient.restoreSession()

        if case .loggedOut = authState {
            await sessionStore.setHasPersistedSession(false)
        }
    }

    /// Returns a typed `LoginResult` so the UI can distinguish invalid
    /// credentials from connectivity, timeout, server, and unexpected errors
    /// without exposing raw API errors directly to the user.
    @discardableResult
    public func login(
        email: String,
        password: String
    ) async -> LoginResult {
        do {
            let response = try await apiClient.login(
                email: email,
                password: password
            )

            await sessionStore.setHasPersistedSession(true)

            authState = AuthSessionReducer.onLoginSuccess(
                accessToken: response.accessToken
            )

            return .success

        } catch let error as RenovexAPIError {
            await sessionStore.setHasPersistedSession(false)
            authState = AuthSessionReducer.onLogout()

            switch error {
            case .httpError(let statusCode): 
                switch statusCode {
                    case 401:
                        return .invalidCredentials
                    
                    case 500...599:
                        return .serverError
                    
                    default:
                        return .unknownError
                }
            case .invalidResponse: 
                return .unknownError
            
            case .decodingFailed: 
                return .unknownError
            }
        
        } catch let error as URLError {
            await sessionStore.setHasPersistedSession(false)
            authState = AuthSessionReducer.onLogout()

            switch error.code {
            case .notConnectedToInternet,
                 .networkConnectionLost,
                 .cannotConnectToHost,
                 .cannotFindHost,
                 .dnsLookupFailed:
                return .networkUnavailable
            
            case .timedOut:
                return .timedOut

            default:
                return .unknownError
            
            }
        } catch {
            await sessionStore.setHasPersistedSession(false)
            authState = AuthSessionReducer.onLogout()
        

            return .unknownError
        }
    }

    public func logout() async {
        await apiClient.logout()

        await sessionStore.setHasPersistedSession(false)

        authState = AuthSessionReducer.onLogout()
        selection = .empty
    }

    public func selectProject(
        id: String,
        name: String
    ) {
        selection = selection.withProject(
            id: id,
            name: name
        )
    }

    public func selectSpace(
        id: String,
        name: String
    ) {
        selection = selection.withSpace(
            id: id,
            name: name
        )
    }

    // MARK: - Read Access for Screens

    public func listProjects() async throws
        -> PaginatedResponseBody<ProjectDTO> {
        try await apiClient.listProjects()
    }

    public func listSpaces(
        projectID: String
    ) async throws -> PaginatedResponseBody<SpaceDTO> {
        try await apiClient.listSpaces(
            projectID: projectID
        )
    }
}