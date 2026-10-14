---
key: partial.findings/no_error_boundary
version: 1
---
### The React app has no error boundary

Why it matters: One rendering error unmounts the whole tree and leaves the user on a blank screen, with nothing reported.

How to fix: Add an error boundary at the app root, and around risky sub-trees, that shows a fallback and reports the error. With the Next.js app router, add error.tsx and global-error.tsx.
