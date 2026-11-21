<!-- Adapted for pergent from alibaba/open-code-review internal/config/rules/rule_docs/properties.md at 39515dc (Apache-2.0, see NOTICE). -->
#### Obvious Typos or Spelling Errors
- Spelling errors in key names, especially the standard spelling of common configuration items

#### Configuration Error Detection
- Duplicate key definitions within the visible scope of the current file causing configuration override issues
- Malformed key-value pairs (missing equals sign, extra whitespace, etc.)
- Special characters not properly escaped (e.g., backslashes in paths, Unicode characters, etc.)

#### Critical Security Issues
- Sensitive information (passwords, API keys, database connection strings, etc.) stored in plaintext
