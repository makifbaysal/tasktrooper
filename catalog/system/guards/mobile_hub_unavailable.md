---
key: guard.mobile_hub_unavailable
version: 1
inputs: [Detail]
---
the Appium hub on this machine could not be started, so no device can be driven ({{trunc 1200 .Detail}}) — this is a problem with the machine's Appium install, not with the app under test; do not retry the mobile tools in this run, report it so a human can fix Appium
