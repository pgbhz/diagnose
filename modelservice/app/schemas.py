from __future__ import annotations

from pydantic import BaseModel, Field


class TrainRequest(BaseModel):
    epochs: int = Field(default=10, ge=1, le=100)
    patience: int = Field(default=3, ge=1, le=20)
    learning_rate: float = Field(default=1e-4, gt=0)
    batch_size: int | None = Field(default=None, ge=4, le=512)
    augment: bool = True


class TrainMetrics(BaseModel):
    epochs_trained: int
    val_accuracy: float
    val_loss: float


class TrainResponse(BaseModel):
    baseline: TrainMetrics
    transfer: TrainMetrics
    weights_path: str


class PredictRequest(BaseModel):
    image_path: str


class PredictResponse(BaseModel):
    label: str
    probability: float
    raw_scores: dict[str, float]
    weights_version: str | None = None
