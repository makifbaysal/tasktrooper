---
key: partial.findings/force_unwrap
version: 1
---
### Force unwraps crash on unexpected nil

Why it matters: try!, as! and !! turn every unexpected nil or error into a crash.

How to fix: Handle the absent case where it can happen: guard let / if let, as?, ?. and ?:, or proper error handling at the boundary. Keep a force unwrap only where nil is truly impossible.
