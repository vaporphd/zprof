"""Calculator module — shakedown fixture.

Intentionally missing: input validation on divide (division by zero).
The shakedown task is to add that validation.
"""


def add(a: float, b: float) -> float:
    return a + b


def subtract(a: float, b: float) -> float:
    return a - b


def multiply(a: float, b: float) -> float:
    return a * b


def divide(a: float, b: float) -> float:
    return a / b
