import math

import pytest

from ai_service.encoder import EmbeddingError, Kind, embed, normalise

from .conftest import StubModel


def test_normalise_gives_unit_length():
    v = normalise([3.0, 4.0], 2)
    assert v == [0.6, 0.8]


@pytest.mark.parametrize("vector", [[0.0, 0.0], [math.nan, 1.0], [math.inf, 1.0]])
def test_normalise_refuses_zero_and_non_finite(vector):
    with pytest.raises(EmbeddingError):
        normalise(vector, 2)


def test_normalise_refuses_the_wrong_size():
    with pytest.raises(EmbeddingError):
        normalise([1.0] * 767, 768)


def test_wrong_dimension_model_is_refused():
    with pytest.raises(EmbeddingError):
        embed(StubModel(dimensions=384), Kind.PASSAGE, ["t"])


def test_missing_vectors_are_refused():
    class Short(StubModel):
        def encode(self, texts):
            return super().encode(texts)[:-1]

    with pytest.raises(EmbeddingError):
        embed(Short(), Kind.QUERY, ["a", "b"])
