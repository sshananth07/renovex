"""Shared response metadata for all three suggestion endpoints."""

from pydantic import BaseModel, ConfigDict


class GenerationMetadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    provider: str
    model: str
    promptVersion: str
    schemaVersion: int
