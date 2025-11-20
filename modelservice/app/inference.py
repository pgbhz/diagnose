from __future__ import annotations

import threading
from pathlib import Path
from typing import Tuple

import numpy as np
import tensorflow as tf

from .config import ASSETS_DIR, WEIGHTS_PATH
from .modeling import build_transfer_model


class TransferModelRegistry:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._model: tf.keras.Model | None = None
        self._weights_digest: str | None = None

    def reset(self) -> None:
        with self._lock:
            self._model = None
            self._weights_digest = None

    def _load_model(self) -> tf.keras.Model:
        if not WEIGHTS_PATH.exists():
            raise FileNotFoundError(
                "Weights file missing. Train the transfer model before inference."
            )
        if self._model is None:
            model = build_transfer_model()
            model.load_weights(str(WEIGHTS_PATH))
            self._model = model
            self._weights_digest = str(WEIGHTS_PATH.stat().st_mtime_ns)
        return self._model

    def predict(self, image: np.ndarray) -> Tuple[float, float]:
        model = self._load_model()
        batch = np.expand_dims(image, axis=0)
        predictions = model.predict(batch, verbose=0).flatten()
        prob = float(predictions[0])
        return prob, 1.0 - prob

    @property
    def weights_version(self) -> str | None:
        with self._lock:
            return self._weights_digest


def resolve_asset_path(raw_path: str) -> Path:
    if not raw_path:
        raise ValueError("image_path must be provided")
    candidate = Path(raw_path)
    if candidate.is_absolute():
        resolved = candidate.resolve()
    else:
        cleaned = candidate
        if cleaned.parts and cleaned.parts[0] == "assets":
            cleaned = Path(*cleaned.parts[1:])
        resolved = (ASSETS_DIR / cleaned).resolve()
    try:
        resolved.relative_to(ASSETS_DIR)
    except ValueError as exc:
        raise ValueError("image_path must reside under the assets directory") from exc
    if not resolved.exists():
        raise FileNotFoundError(f"Image not found: {resolved}")
    return resolved