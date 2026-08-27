# Claude Reviewer Plugin

This document provides details on the Claude Reviewer plugin for the merge-bot.

## Overview

The Claude Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the Anthropic Claude API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides comprehensive feedback with inline thread comments.
-   **Autonomous Agent Loop & Tool Calling**: Claude can call the host functions `get_git_file` (full file content), `search_code` (repository search, up to 100 results), `fetch_web_content` (documentation from approved domains), and `get_ci_job_log` (fetch CI job log by ID) when it needs more context than the diff.
-   **Configurable**: The plugin can be configured with different models, prompts, endpoints, max tokens, Anthropic API versions, and agent turn limits.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                                | Description                                                               | Type           | Default Value                              |
| ----------------------------------- | ------------------------------------------------------------------------- | -------------- | ------------------------------------------ |
| `claude_reviewer_api_key`           | Your Anthropic Claude API key.                                            | `env`, `secret` | (none)                                     |
| `claude_reviewer_endpoint`          | The endpoint for the Claude API.                                          | `env`, `secret` | `https://api.anthropic.com/v1/messages`    |
| `claude_reviewer_model`             | The Claude model to use for the review.                                   | `env`, `config` | `claude-sonnet-5`                          |
| `claude_reviewer_prompt`            | A custom prompt to use for the review.                                    | `env`, `config` | (see code)                                 |
| `claude_reviewer_max_tokens`        | Max tokens to generate. Values below `8192` fall back to the default.     | `env`, `config` | `16000`                                    |
| `claude_reviewer_anthropic_version` | The Anthropic API version to use.                                         | `env`, `config` | `2023-06-01`                               |
| `claude_reviewer_max_turns`         | Max agent-loop turns. Values below `2` fall back to the default.          | `env`, `config` | `20`                                       |
| `claude_reviewer_max_retries`       | Maximum retry attempts on transient errors (503, 429, 5xx) with backoff.  | `env`, `config` | `5`                                        |

## Build

To compile the plugin to WebAssembly:

```bash
cd plugins/claude-reviewer
GOOS="wasip1" GOARCH="wasm" go build -o ../../claude-plugin.wasm -buildmode=c-shared main.go
```

## Test

`go test ./...` does not work from the repository root: every package that imports
`github.com/extism/go-pdk` fails to build for the host, because the PDK's host
functions have no bodies outside wasm. The JSON wire format lives in
`internal/wire`, which imports only `encoding/json`, so it is host-testable:

```bash
go test ./plugins/claude-reviewer/internal/...
```

Tests in `main_test.go` are compile-checked only, via
`GOOS="wasip1" GOARCH="wasm" go test -c -o /dev/null ./plugins/claude-reviewer/`.
A wasip1 test binary compiles but cannot be executed by a plain wasm runtime,
since it declares `extism:host/env` imports that runtime cannot resolve.

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `claude_reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `claude-reviewer.yaml` file.
