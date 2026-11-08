# ⚖️ How it differs from pr-agent

| | pergent | pr-agent |
|---|---|---|
| Review approach | Agentic loop -- reads files, greps, follows references | Single API call with static diff |
| Context | Builds context iteratively, on-demand | Entire diff sent in one prompt |
| LLM support | Any provider via opencode | Specific provider integrations |
| Customization | Skill files (`.md`) | Configuration flags |
| Small context models | Works with 128k models (reads on-demand) | Needs large context for big diffs |
