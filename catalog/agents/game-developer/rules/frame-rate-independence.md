---
name: frame-rate-independence
priority: 85
enabled: true
---
Every time-based value is scaled by the frame or step delta — movement, rotation, cooldowns, timers, animation blends, lerp smoothing (use `1 - exp(-k * dt)`, not a constant factor) — and nothing counts frames to measure time. A behaviour must read the same at 30, 60, 144 FPS and unlocked: test it at two different fixed deltas and assert the same outcome within tolerance, and never rely on vsync or a frame cap to make a speed right.
