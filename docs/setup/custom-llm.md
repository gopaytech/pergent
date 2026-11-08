# Custom LLM (LiteLLM, vLLM, Ollama)

pergent works with any OpenAI-compatible API. Set `OPENCODE_BASE_URL` to your endpoint and pergent automatically uses `/chat/completions` instead of `/responses`:

```bash
export OPENCODE_PROVIDER=openai
export OPENCODE_API_KEY=your-key
export OPENCODE_MODEL=your-model
export OPENCODE_BASE_URL=https://your-proxy.example.com/v1

pergent --local --skill code-review
```
