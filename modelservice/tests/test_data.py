import logging
from pathlib import Path

import cv2
import numpy as np
import pytest

from modelservice.app import data


def _write_image(path: Path, color: tuple[int, int, int] = (0, 0, 255)) -> None:
    array = np.full((64, 64, 3), color, dtype=np.uint8)
    success = cv2.imwrite(str(path), array)
    if not success:
        raise RuntimeError(f"Failed to create test image at {path}")


def test_load_image_returns_normalized_array(tmp_path: Path) -> None:
    image_path = tmp_path / "sample.png"
    _write_image(image_path, (0, 255, 0))

    loaded = data.load_image(image_path)

    assert loaded.shape == (data.TARGET_SIZE[0], data.TARGET_SIZE[1], 3)
    assert loaded.dtype == np.float32
    assert 0.0 <= loaded.min() <= loaded.max() <= 1.0
    assert np.isclose(loaded.max(), 1.0)


def test_load_image_raises_for_unreadable_file(tmp_path: Path) -> None:
    invalid_path = tmp_path / "broken.jpeg"
    invalid_path.write_text("not-an-image")

    with pytest.raises(ValueError):
        data.load_image(invalid_path)


def test_load_dataset_skips_unreadable_images(tmp_path: Path, caplog: pytest.LogCaptureFixture) -> None:
    root_dir = tmp_path / "dataset"
    class_c = root_dir / "c"
    class_nc = root_dir / "nc"
    class_c.mkdir(parents=True)
    class_nc.mkdir()

    _write_image(class_c / "valid.png", (255, 0, 0))
    (class_c / "invalid.jpeg").write_text("oops")
    _write_image(class_nc / "valid.png", (0, 0, 255))

    caplog.set_level(logging.WARNING, logger="modelservice.app.data")

    images, labels = data.load_dataset(root_dir)

    assert images.shape[0] == 2
    assert labels.shape[0] == 2
    assert sorted(labels.tolist()) == [0, 1]
    assert "Skipped 1 unreadable images" in caplog.text
