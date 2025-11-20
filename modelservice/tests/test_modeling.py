import numpy as np
import pytest
import tensorflow as tf

from modelservice.app import modeling


def test_transfer_model_rescales_unit_range_inputs() -> None:
    model = modeling.build_transfer_model(weights=None)
    rescale = model.get_layer("input_rescale")
    assert np.isclose(rescale.scale, 2.0)
    assert np.isclose(rescale.offset, -1.0)

    probe = tf.constant(
        np.stack(
            [
                np.zeros((modeling.IMAGE_SIZE, modeling.IMAGE_SIZE, 3), dtype=np.float32),
                np.ones((modeling.IMAGE_SIZE, modeling.IMAGE_SIZE, 3), dtype=np.float32),
            ]
        ),
        dtype=tf.float32,
    )
    probe_model = tf.keras.Model(model.input, rescale.output)
    rescaled = probe_model.predict(probe, verbose=0)
    assert rescaled.min() == pytest.approx(-1.0, rel=1e-4)
    assert rescaled.max() == pytest.approx(1.0, rel=1e-4)
