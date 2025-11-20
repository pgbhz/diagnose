from __future__ import annotations

import logging
from pathlib import Path
from typing import Tuple

import cv2
import numpy as np

from .config import IMAGE_SIZE

CLASS_MAP = {"c": 1, "nc": 0}
TARGET_SIZE = (IMAGE_SIZE, IMAGE_SIZE)


def load_image(path: Path) -> np.ndarray:
    image = cv2.imread(str(path))
    if image is None:
        raise ValueError(f"Unable to read image: {path}")
    image = cv2.cvtColor(image, cv2.COLOR_BGR2RGB)
    image = cv2.resize(image, TARGET_SIZE)
    return image.astype("float32") / 255.0


def _gather_files(root_dir: Path) -> Tuple[list[Path], list[int]]:
    images: list[Path] = []
    labels: list[int] = []
    for class_name, label in CLASS_MAP.items():
        class_dir = root_dir / class_name
        if not class_dir.exists():
            continue
        for item in class_dir.iterdir():
            if item.is_file():
                images.append(item)
                labels.append(label)
    return images, labels


def load_dataset(root_dir: Path) -> Tuple[np.ndarray, np.ndarray]:
    file_paths, labels = _gather_files(root_dir)
    if not file_paths:
        raise FileNotFoundError(f"No images found under {root_dir}")

    images: list[np.ndarray] = []
    filtered_labels: list[int] = []
    skipped: list[Path] = []

    for path, label in zip(file_paths, labels):
        try:
            images.append(load_image(path))
            filtered_labels.append(label)
        except ValueError:
            skipped.append(path)

    if skipped:
        logging.getLogger(__name__).warning(
            "Skipped %d unreadable images under %s", len(skipped), root_dir
        )

    if not images:
        raise ValueError(f"All images under {root_dir} failed to load")

    return np.stack(images), np.array(filtered_labels, dtype="int32")
