from __future__ import annotations

from dataclasses import dataclass
from typing import Tuple

import tensorflow as tf

from . import data, modeling
from .config import (
    BATCH_SIZE,
    TRAIN_DATA_DIR,
    VALIDATION_DATA_DIR,
    WEIGHTS_PATH,
)
from .schemas import TrainMetrics, TrainRequest, TrainResponse


def _train_model(
    model: tf.keras.Model,
    train_ds: tf.data.Dataset,
    val_ds: tf.data.Dataset,
    patience: int,
    epochs: int,
) -> Tuple[int, float, float]:
    stopper = modeling.build_early_stopping(patience)
    history = model.fit(train_ds, validation_data=val_ds, epochs=epochs, callbacks=[stopper])
    trained_epochs = len(history.history["loss"])
    val_loss, val_accuracy = model.evaluate(val_ds, verbose=0)
    return trained_epochs, float(val_accuracy), float(val_loss)


def train_all(request: TrainRequest) -> TrainResponse:
    train_images, train_labels = data.load_dataset(TRAIN_DATA_DIR)
    val_images, val_labels = data.load_dataset(VALIDATION_DATA_DIR)

    batch_size = request.batch_size or BATCH_SIZE

    train_ds = modeling.make_dataset(
        train_images,
        train_labels,
        training=True,
        augment=request.augment,
        batch_size=batch_size,
    )
    val_ds = modeling.make_dataset(
        val_images,
        val_labels,
        training=False,
        augment=False,
        batch_size=batch_size,
    )

    baseline_model = modeling.build_baseline_model()
    baseline_epochs, baseline_val_acc, baseline_val_loss = _train_model(
        baseline_model,
        train_ds,
        val_ds,
        request.patience,
        request.epochs,
    )

    transfer_model = modeling.build_transfer_model(request.learning_rate)
    transfer_epochs, transfer_val_acc, transfer_val_loss = _train_model(
        transfer_model,
        train_ds,
        val_ds,
        request.patience,
        request.epochs,
    )

    transfer_model.save_weights(str(WEIGHTS_PATH))

    return TrainResponse(
        baseline=TrainMetrics(
            epochs_trained=baseline_epochs,
            val_accuracy=baseline_val_acc,
            val_loss=baseline_val_loss,
        ),
        transfer=TrainMetrics(
            epochs_trained=transfer_epochs,
            val_accuracy=transfer_val_acc,
            val_loss=transfer_val_loss,
        ),
        weights_path=str(WEIGHTS_PATH),
    )
