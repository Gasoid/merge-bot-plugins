# OpenAI Reviewer Plugin

This document provides details on the OpenAI Reviewer plugin for the merge-bot.

## Overview

The OpenAI Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the OpenAI API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides feedback.
-   **Configurable**: The plugin can be configured with different models, prompts, and endpoints.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                         | Description                                                                 | Type          | Default Value |
| ---------------------------- | --------------------------------------------------------------------------- | ------------- | ------------- |
| `reviewer_api_key`    | Your OpenAI API key.                                                 | `env`, `secret` | (none)        |
| `reviewer_endpoint`   | The endpoint for the OpenAI API.                                            | `env`, `secret` | (see code)    |
| `reviewer_model`      | The OpenAI model to use for the review.                                     | `env`, `config` | `gpt-5.1-codex-mini`  |
| `reviewer_prompt`     | A custom prompt to use for the review.                                      | `env`, `config` | (see code)    |

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `openai-reviewer.yaml` file.
