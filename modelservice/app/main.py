from __future__ import annotations

import asyncio
import logging
from typing import Dict

from fastapi import FastAPI, HTTPException

from . import data
from .config import ASSETS_DIR, TRAIN_DATA_DIR, VALIDATION_DATA_DIR
from .inference import TransferModelRegistry, resolve_asset_path
from .schemas import PredictRequest, PredictResponse, TrainRequest, TrainResponse
from .trainer import train_all

logger = logging.getLogger("modelservice")
logging.basicConfig(level=logging.INFO)

app = FastAPI(title="Diagnosis Model Service", version="0.1.0")
registry = TransferModelRegistry()


@app.get("/healthz")
def healthcheck() -> Dict[str, str]:
    return {
        "status": "ok",
        "train_data": str(TRAIN_DATA_DIR),
        "validation_data": str(VALIDATION_DATA_DIR),
        "assets_dir": str(ASSETS_DIR),
    }


@app.post("/train", response_model=TrainResponse)
async def train_models(payload: TrainRequest) -> TrainResponse:
    try:
        response = await asyncio.to_thread(train_all, payload)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    registry.reset()
    return response


@app.post("/predict", response_model=PredictResponse)
async def classify_image(request: PredictRequest) -> PredictResponse:
    try:
        image_path = resolve_asset_path(request.image_path)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    image = data.load_image(image_path)

    try:
        positive_prob, negative_prob = registry.predict(image)
    except FileNotFoundError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    label = "c" if positive_prob >= 0.5 else "nc"
    return PredictResponse(
        label=label,
        probability=positive_prob if label == "c" else negative_prob,
        raw_scores={"c": positive_prob, "nc": negative_prob},
        weights_version=registry.weights_version,
    )


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "modelservice.app.main:app",
        host="0.0.0.0",
        port=int("8080"),
    )
