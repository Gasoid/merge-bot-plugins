# Plugins for Merge-Bot

This repository contains plugins for Merge-Bot, implemented in Go and compiled to WebAssembly (WASM) using the `wasip1` target.

Link to Merge-Bot docs: https://github.com/Gasoid/merge-bot/blob/main/plugins.md

## Available Plugins

This repository contains the following plugins:

-   **[OpenAI Reviewer](./plugins/openai-reviewer/README.md)**: A plugin that uses the OpenAI API to review merge requests.
-   **[Gemini Reviewer](./plugins/gemini-reviewer/README.md)**: A plugin that uses the Google Gemini API to review merge requests.
-   **[Claude Reviewer](./plugins/claude-reviewer/README.md)**: A plugin that uses the Anthropic Claude API to review merge requests.

For more information about each plugin, please refer to their respective `README.md` files.

## Installation

To use a plugin, you need to configure your Merge-Bot instance by setting the `PLUGINS` environment variable. This variable should point to the plugin's YAML configuration file.

For example, to install the **OpenAI Reviewer** plugin, you would set the following environment variables:

```bash
export PLUGINS="https://github.com/Gasoid/merge-bot-plugins/releases/download/v0.0.2/openai-reviewer.yaml"
export REVIEWER_API_KEY="your_openai_api_key"
```

Please note that each plugin has its own set of required environment variables for configuration (like API keys). For detailed installation and configuration instructions, please refer to the `README.md` file of the specific plugin you want to use.

## Build
standard go:
```sh
GOOS="wasip1" GOARCH="wasm" go build -o plugin.wasm -buildmode=c-shared main.go
```
