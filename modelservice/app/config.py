from __future__ import annotations

import os
from pathlib import Path

SERVICE_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_DATA_DIR = SERVICE_ROOT.parent / "data"
DEFAULT_MODEL_DIR = SERVICE_ROOT / "model"
DEFAULT_ASSETS_DIR = SERVICE_ROOT.parent / "src" / "assets"

IMAGE_SIZE = int(os.getenv("IMAGE_SIZE", "128"))
BATCH_SIZE = int(os.getenv("BATCH_SIZE", "32"))
SEED = int(os.getenv("MODEL_SEED", "42"))

TRAIN_DATA_DIR = Path(
    os.getenv("TRAIN_DATA_DIR", str(DEFAULT_DATA_DIR / "train"))
).resolve()
VALIDATION_DATA_DIR = Path(
    os.getenv("VALIDATION_DATA_DIR", str(DEFAULT_DATA_DIR / "validation"))
).resolve()
ASSETS_DIR = Path(os.getenv("ASSETS_DIR", str(DEFAULT_ASSETS_DIR))).resolve()
MODEL_DIR = Path(os.getenv("MODEL_DIR", str(DEFAULT_MODEL_DIR))).resolve()
WEIGHTS_PATH = Path(
    os.getenv(
        "WEIGHTS_PATH",
        str(MODEL_DIR / "weights" / "mobilenet_transfer.weights.h5"),
    )
).resolve()

MODEL_DIR.mkdir(parents=True, exist_ok=True)
WEIGHTS_PATH.parent.mkdir(parents=True, exist_ok=True)
