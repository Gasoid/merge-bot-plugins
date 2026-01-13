# Plugins for Merge-Bot

This repository contains plugins for Merge-Bot, implemented in Go and compiled to WebAssembly (WASM) using the `wasip1` target.

Link to Merge-Bot docs: https://github.com/Gasoid/merge-bot/blob/main/plugins.md

## Available Plugins

This repository contains the following plugins:

-   **[OpenAI Reviewer](./plugins/openai-reviewer/README.md)**: A plugin that uses the OpenAI API to review merge requests.
-   **[Gemini Reviewer](./plugins/gemini-reviewer/README.md)**: A plugin that uses the Google Gemini API to review merge requests.

For more information about each plugin, please refer to their respective `README.md` files.

## Build
standard go:
```sh
GOOS="wasip1" GOARCH="wasm" go build -o plugin.wasm -buildmode=c-shared main.go
```
