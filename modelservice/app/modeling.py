from __future__ import annotations

import tensorflow as tf
from keras import Input, Model, Sequential, applications, callbacks, layers, optimizers

from .config import BATCH_SIZE, IMAGE_SIZE, SEED

INPUT_SHAPE = (IMAGE_SIZE, IMAGE_SIZE, 3)

_augmentor = Sequential(
    [
        layers.RandomRotation(0.042, seed=SEED),
        layers.RandomTranslation(0.05, 0.05, seed=SEED),
        layers.RandomZoom(0.1, 0.1, seed=SEED),
        layers.RandomFlip("horizontal", seed=SEED),
    ],
    name="augmentor",
)


def make_dataset(images, labels, *, training: bool, augment: bool, batch_size: int | None = None) -> tf.data.Dataset:
    batch = batch_size or BATCH_SIZE
    dataset = tf.data.Dataset.from_tensor_slices((images, labels))
    if training:
        dataset = dataset.shuffle(buffer_size=len(images), seed=SEED, reshuffle_each_iteration=True)
        if augment:
            dataset = dataset.map(
                lambda x, y: (_augmentor(x, training=True), y),
                num_parallel_calls=tf.data.AUTOTUNE,
            )
    return dataset.batch(batch).prefetch(tf.data.AUTOTUNE)


def build_baseline_model() -> Model:
    model = Sequential(
        [
            Input(shape=INPUT_SHAPE),
            layers.Conv2D(32, (3, 3), activation="relu"),
            layers.MaxPooling2D(2, 2),
            layers.Conv2D(64, (3, 3), activation="relu"),
            layers.MaxPooling2D(2, 2),
            layers.Flatten(),
            layers.Dense(128, activation="relu"),
            layers.Dropout(0.5),
            layers.Dense(1, activation="sigmoid"),
        ]
    )
    model.compile(optimizer="adam", loss="binary_crossentropy", metrics=["accuracy"])
    return model


def build_transfer_model(learning_rate: float = 1e-4, weights: str | None = "imagenet") -> Model:
    base_model = applications.MobileNetV2(
        input_shape=INPUT_SHAPE,
        include_top=False,
        weights=weights,
    )
    base_model.trainable = False

    inputs = Input(shape=INPUT_SHAPE)
    x = layers.Rescaling(scale=2.0, offset=-1.0, name="input_rescale")(inputs)
    x = base_model(x, training=False)
    x = layers.GlobalAveragePooling2D()(x)
    x = layers.Dropout(0.3)(x)
    outputs = layers.Dense(1, activation="sigmoid")(x)
    model = Model(inputs, outputs, name="mobilenet_transfer")
    model.compile(
        optimizer=optimizers.Adam(learning_rate=learning_rate),
        loss="binary_crossentropy",
        metrics=["accuracy"],
    )
    return model


def build_early_stopping(patience: int) -> callbacks.EarlyStopping:
    return callbacks.EarlyStopping(
        monitor="val_loss",
        patience=patience,
        restore_best_weights=True,
    )
