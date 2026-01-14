# Deepseek Reviewer Plugin

This document provides details on the Deepseek Reviewer plugin for the merge-bot.

## Overview

The Deepseek Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the Deepseek API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides feedback.
-   **Configurable**: The plugin can be configured with different models, prompts, endpoints, and max tokens.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                           | Description                                                                 | Type          | Default Value                               |
| ------------------------------ | --------------------------------------------------------------------------- | ------------- | ------------------------------------------- |
| `deepseek_reviewer_api_key`    | Your Deepseek API key.                                                      | `env`, `secret` | (none)                                      |
| `deepseek_reviewer_endpoint`   | The endpoint for the Deepseek API.                                          | `env`, `secret` | `https://api.deepseek.com/chat/completions` |
| `deepseek_reviewer_model`      | The Deepseek model to use for the review.                                   | `env`, `config` | `deepseek-chat`                             |
| `deepseek_reviewer_prompt`     | A custom prompt to use for the review.                                      | `env`, `config` | (see code)                                  |
| `deepseek_reviewer_max_tokens` | The maximum number of tokens to generate in the response.                   | `env`, `config` | `1024`                                      |

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `deepseek_reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `deepseek-reviewer.yaml` file.
