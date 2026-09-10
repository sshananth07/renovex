"""Stable, machine-readable internal error classes (design doc "Error Model").
Python never leaks raw provider exceptions/stack traces to Go; Go maps these
codes into the public Huma problem-details model."""


class AIServiceError(Exception):
    """Base class for typed internal-service errors. code is a stable
    identifier Go can switch on without parsing English text."""

    code = "AI_SERVICE_UNAVAILABLE"

    def __init__(self, message: str = ""):
        super().__init__(message)


class InvalidAIRequest(AIServiceError):
    code = "INVALID_AI_REQUEST"


class ProviderUnavailable(AIServiceError):
    code = "PROVIDER_UNAVAILABLE"


class ProviderRateLimited(AIServiceError):
    code = "PROVIDER_RATE_LIMITED"


class ProviderTimeout(AIServiceError):
    code = "PROVIDER_TIMEOUT"


class InvalidProviderResponse(AIServiceError):
    code = "INVALID_PROVIDER_RESPONSE"
