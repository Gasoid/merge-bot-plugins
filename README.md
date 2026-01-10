# Plugins for Merge-Bot

This repository contains plugins for Merge-Bot, implemented in Go and compiled to WebAssembly (WASM) using the `wasip1` target.

Link to Merge-Bot docs: https://github.com/Gasoid/merge-bot/blob/main/plugins.md

## OpenAI Plugin
This plugin integrates OpenAI's GPT models to provide AI-powered reviews and suggestions for code changes.

### Prerequisites
- Merge-Bot version >= 3.8.0

### Configuration
To use the OpenAI plugin, set the following environment variables:
- `REVIEWER_API_KEY`: Your OpenAI API key.
- `REVIEWER_MODEL`: The OpenAI model to use (default is gpt-5.1-codex-mini).
- `REVIEWER_PROMPT`: The prompt template for generating reviews.
- `REVIEWER_ENDPOINT`: (Optional) Custom OpenAI API endpoint. For instance, use `https://your-instance.openai.azure.com/openai/responses?api-version=2024-02-15-preview` for Azure OpenAI.

### Installation
set env variables of merge-bot to use this plugin:
```bash
export PLUGINS="https://github.com/Gasoid/merge-bot-plugins/blob/main/openai-reviewer/openai-reviewer.yaml"
export REVIEWER_API_KEY="your_openai_api_key"
```

After instalation, Merge-Bot will use the OpenAI plugin to review pull requests.
Use command: `!review` in pull request comments to trigger a review.

You can customize the prompt and model by setting the respective environment variables.
You can set variables in .mrbot.yaml file of repository too.

e.g.
```yaml
plugin_vars:
  reviewer_prompt: "Your custom prompt here"
  reviewer_model: "gpt-5.1-codex-mini"
```

OpenAI api key can be set in repository secrets as well.
CI/CD variable name should be: `REVIEWER_API_KEY`.

see plugin manifest for more details: [openai-reviewer.yaml](openai-reviewer/openai-reviewer.yaml)

## Build
standard go:
```sh
GOOS="wasip1" GOARCH="wasm" go build -o plugin.wasm -buildmode=c-shared main.go
```
