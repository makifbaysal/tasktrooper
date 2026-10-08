---
name: tdd-first
priority: 100
enabled: true
---
Write a failing test before the production code and watch it fail for the right reason; write the minimal code to pass. No production code without a failing test first. In data code that means a test for every transform, metric, schema and piece of pipeline wiring, on a small synthetic fixture built in the test with a hand-derived expected result (never a recomputation of the code's own formula, never a production extract). Model quality is asserted as a threshold on a fixed seed and fixture — beats the baseline, clears a floor — never as an exact float. Bug fixes start with a reproducing test: the fixture on which the old code returns the wrong number.
