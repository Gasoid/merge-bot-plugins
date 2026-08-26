# OpenAI Reviewer Plugin

This document provides details on the OpenAI Reviewer plugin for the merge-bot.

## Overview

The OpenAI Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the OpenAI API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides comprehensive feedback with inline thread comments.
-   **Autonomous Agent Loop & Tool Calling**: OpenAI can call the host functions `get_git_file` (full file content), `search_code` (repository search, up to 100 results), `fetch_web_content` (documentation from approved domains), and `get_ci_failed_jobs` (failed CI job logs) when it needs more context than the diff.

> **Note for reasoning models** (e.g. `gpt-5.x` variants): the `chat/completions` endpoint rejects function tools when `reasoning_effort` is set. The plugin therefore defaults `reasoning_effort` to `none` on tool-calling turns (override with `reviewer_reasoning_effort`).
-   **Configurable**: The plugin can be configured with different models, prompts, endpoints, and agent turn limits.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                         | Description                                                                 | Type          | Default Value |
| ---------------------------- | --------------------------------------------------------------------------- | ------------- | ------------- |
| `reviewer_api_key`    | Your OpenAI API key.                                                 | `env`, `secret` | (none)        |
| `reviewer_endpoint`   | The endpoint for the OpenAI API.                                            | `env`, `secret` | https://api.openai.com/v1/chat/completions |
| `reviewer_model`      | The OpenAI model to use for the review.                                     | `env`, `config` | `gpt-5.1-codex-mini`  |
| `reviewer_prompt`     | A custom prompt to use for the review.                                      | `env`, `config` | (see code)    |
| `reviewer_max_turns`  | Maximum tool-calling conversation turns in the agent loop.                  | `env`, `config` | `20`           |
| `reviewer_max_retries`| Maximum retry attempts on transient errors (503, 429, 5xx) with backoff.   | `env`, `config` | `5`           |
| `reviewer_reasoning_effort` | Reasoning effort for models that support it. When tools are used, defaults to `none` so reasoning models work on `/v1/chat/completions`. | `env`, `config` | `none` (when tools are used) |

## Build

To compile the plugin to WebAssembly:

```bash
cd plugins/openai-reviewer
GOOS="wasip1" GOARCH="wasm" go build -o ../../openai-plugin.wasm -buildmode=c-shared main.go
```

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `openai-reviewer.yaml` file.
