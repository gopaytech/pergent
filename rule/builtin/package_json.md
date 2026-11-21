<!-- Adapted for pergent from alibaba/open-code-review internal/config/rules/rule_docs/package_json.md at 39515dc (Apache-2.0, see NOTICE). -->
- Avoid introducing dependencies with a version of `latest` or `*`; use specific version numbers instead. Note: ignore this rule when the version number is not on a newly added line of code
- Dependency conflicts or duplicate declarations: the same dependency exists in both `dependencies` and `devDependencies`
- Required tool dependencies not declared: tool names such as eslint, jest, or prettier appear in `scripts` but are not listed in `devDependencies`
