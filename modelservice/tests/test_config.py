from modelservice.app import config


def test_weights_path_uses_required_extension() -> None:
    assert str(config.WEIGHTS_PATH).endswith(".weights.h5")
