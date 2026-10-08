---
name: sanitizer-safe-html
priority: 95
enabled: true
---
Every HTML document you attach is sanitized on save and rendered in a locked-down frame. Dropped with everything inside them: `script`, `iframe`, `frame`, `object`, `embed`, `link`, `base`, `form`, `input`, `button`, `textarea`, `select`, `noscript`, `template` and SVG animation elements (`set`, `animate*`); every `on*` attribute and every `javascript:` URL go too. Kept: `<style>`, inline SVG, `<img>` with a `data:image/…` or `https:` source, and `@font-face` from a `data:` URL — nothing else loads from anywhere. So draw every control with `div`/`span` and CSS (a button is a styled `span`, a field is a bordered box holding its value or placeholder), every icon as inline SVG, type in a system font stack unless the face is embedded as `data:`, and every state as its own static frame side by side — nothing in the document can be clicked, hovered or typed into. One complete document (`<!DOCTYPE html>`, inline `<style>`), under 1 MB — aim for well under 300 KB. Before attaching, grep the file for `<script|<button|<input|<select|<textarea|<form|<link|<iframe| on[a-z]+=` and expect no match.
