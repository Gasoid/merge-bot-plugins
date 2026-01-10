# Plugins for Merge-Bot

This repository contains plugins for Merge-Bot, implemented in Go and compiled to WebAssembly (WASM) using the `wasip1` target.

Link to Merge-Bot docs: https://github.com/Gasoid/merge-bot/blob/main/plugins.md

## OpenAI Plugin
This plugin integrates OpenAI's GPT models to provide AI-powered reviews and suggestions for code changes.

### Configuration
To use the OpenAI plugin, set the following environment variables:
- `REVIEWER_API_KEY`: Your OpenAI API key.
- `REVIEWER_MODEL`: The OpenAI model to use (default is gpt-5.1-codex-mini).
- `REVIEWER_PROMPT`: The prompt template for generating reviews.
- `REVIEWER_ENDPOINT`: (Optional) Custom OpenAI API endpoint. For instance, use `https://your-instance.openai.azure.com/openai/responses?api-version=2024-02-15-preview` for Azure OpenAI.

## Build
standard go:
```sh
GOOS="wasip1" GOARCH="wasm" go build -o plugin.wasm -buildmode=c-shared main.go
```
