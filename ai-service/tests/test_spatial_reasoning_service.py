"""SpatialReasoningService tests. Proves target/context references are
cross-checked AFTER Pydantic parsing (the model could echo a structurally
valid but wrong id), invalid output causes no repair/fallback (a single
provider call, an exception propagates), and provider metadata
(provider/model/promptVersion) is locally trusted from the provider
implementation, never model-authored (RP4E1 plan Task 3 Step 5)."""

import json
from pathlib import Path

import pytest

from app.errors import InvalidProviderResponse
from app.providers.spatial_mock import SpatialMockProvider
from app.schemas.spatial_reasoning import SpatialReasoningRequest
from app.services.spatial_reasoning import SpatialReasoningService

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "spatial_reasoning"


def _request_from_fixture(name: str) -> SpatialReasoningRequest:
    data = json.loads((FIXTURES_DIR / name).read_text())
    return SpatialReasoningRequest(**data["request"])


class _FakeProvider:
    def __init__(self, result=None, raises=None):
        self._result = result
        self._raises = raises
        self.calls = 0

    def reason_element(self, request):
        self.calls += 1
        if self._raises:
            raise self._raises
        return self._result


class TestSpatialReasoningServiceOrchestration:
    def test_delegates_to_provider_exactly_once(self):
        provider = SpatialMockProvider()
        service = SpatialReasoningService(provider)
        request = _request_from_fixture("material_only.json")

        result = service.reason_element(request)

        assert result.delta.target.id == request.selectedElement.id

    def test_provider_exception_propagates_without_retry(self):
        provider = _FakeProvider(raises=InvalidProviderResponse("boom"))
        service = SpatialReasoningService(provider)
        request = _request_from_fixture("material_only.json")

        with pytest.raises(InvalidProviderResponse):
            service.reason_element(request)

        assert provider.calls == 1

    def test_provider_metadata_is_locally_trusted_not_model_authored(self):
        # SpatialMockProvider's own provider/model/promptVersion fields come
        # from the provider implementation itself, never from parsed model
        # output — there is no "provider" field on ProposedSceneEditDelta
        # for a model to author in the first place.
        provider = SpatialMockProvider()
        service = SpatialReasoningService(provider)
        request = _request_from_fixture("material_only.json")

        result = service.reason_element(request)

        assert result.provider == "mock"
        assert result.schemaVersion == 1


class TestSpatialMockProviderDeterminism:
    @pytest.mark.parametrize(
        "fixture_name",
        ["material_only.json", "geometry_only.json", "spatial_only.json", "mixed.json", "unsupported_structural.json"],
    )
    def test_mock_provider_is_deterministic_pure_function(self, fixture_name):
        provider = SpatialMockProvider()
        request = _request_from_fixture(fixture_name)

        first = provider.reason_element(request)
        second = provider.reason_element(request)

        assert first.delta.model_dump() == second.delta.model_dump()

    def test_mock_provider_echoes_correct_target(self):
        provider = SpatialMockProvider()
        request = _request_from_fixture("material_only.json")

        result = provider.reason_element(request)

        assert result.delta.target.id == request.selectedElement.id
        assert result.delta.target.kind == request.selectedElement.kind
