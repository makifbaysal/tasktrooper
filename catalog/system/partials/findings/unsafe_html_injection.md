---
key: partial.findings/unsafe_html_injection
version: 1
---
### Raw HTML is inserted into the page

Why it matters: dangerouslySetInnerHTML, v-html, innerHTML, document.write and Angular's bypassSecurityTrust* render markup without escaping: user-controlled content becomes cross-site scripting.

How to fix: Render text as text. Where HTML is genuinely needed, sanitize it with an allowlist sanitizer (DOMPurify or the framework's sanitizer) right before rendering, and keep the sanitizing in one shared helper.
