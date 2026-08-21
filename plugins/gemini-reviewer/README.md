# Gemini Reviewer Plugin

This document provides details on the Gemini Reviewer plugin for the merge-bot.

## Overview

The Gemini Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the Google Gemini API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides comprehensive feedback.
-   **Autonomous Agent Loop & Tool Calling**: Gemini can request full file contents via the host function `get_git_file` if more context is needed beyond the diff.
-   **Configurable**: The plugin can be configured with different models, prompts, endpoints, and agent turn limits.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                         | Description                                                                 | Type          | Default Value |
| ---------------------------- | --------------------------------------------------------------------------- | ------------- | ------------- |
| `gemini_reviewer_api_key`    | Your Google Gemini API key.                                                 | `env`, `secret` | (none)        |
| `gemini_reviewer_endpoint`   | The endpoint for the Gemini API.                                            | `env`, `secret` | https://generativelanguage.googleapis.com/v1beta/models/ |
| `gemini_reviewer_model`      | The Gemini model to use for the review.                                     | `env`, `config` | `gemini-2.5-flash-lite`  |
| `gemini_reviewer_prompt`     | A custom prompt to use for the review.                                      | `env`, `config` | (see code)    |
| `gemini_reviewer_max_turns`  | Maximum tool-calling conversation turns in the agent loop.                  | `env`, `config` | `5`           |
| `gemini_reviewer_max_retries`| Maximum retry attempts on transient errors (503, 429, 5xx) with backoff.   | `env`, `config` | `5`           |

## Build

To compile the plugin to WebAssembly:

```bash
cd plugins/gemini-reviewer
GOOS="wasip1" GOARCH="wasm" go build -o ../../gemini-plugin.wasm -buildmode=c-shared main.go
```

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `gemini_reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `gemini-reviewer.yaml` file.
