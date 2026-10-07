---
key: tool.grep_code
version: "1"
params:
    case_sensitive: 'Match case exactly, for both pattern and glob. Default false: the search ignores case, so "coming soon" also finds "Coming Soon" and glob "*.TSX" still matches .tsx files.'
    glob: Optional glob filter for file paths (e.g. *.go)
    max_results: 'Maximum number of matching lines to return (default: 100)'
    path: 'Relative path within the workspace to search (default: workspace root)'
    pattern: Regular expression pattern to search for
---
Search workspace files with a regex pattern (ripgrep syntax; look-around and backreferences are not supported). Case-insensitive by default. Respects .gitignore. A line longer than 300 characters comes back cut short, ending in "[... omitted end of long line]" — read_file the line for the rest.
