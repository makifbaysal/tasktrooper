---
key: guard.automation_move_held
version: 1
inputs: [Target]
---
not moved to {{.Target}}: automation tests are on for this repository, so the board makes this move itself — once your manual round has passed and the automation suite (`qa-automation/scripts/e2e.sh`) is green. Leave the task where it is: make sure every case has a result and every criterion your verdict, add this task's tests under `qa-automation/`, and end your run
