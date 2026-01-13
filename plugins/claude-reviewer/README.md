# Claude Reviewer Plugin

This document provides details on the Claude Reviewer plugin for the merge-bot.

## Overview

The Claude Reviewer is a WebAssembly (WASM) plugin that integrates with the merge-bot to provide automated code reviews for merge requests in GitLab. It leverages the Anthropic Claude API to analyze code changes and suggest improvements.

## Features

-   **Automated Code Reviews**: Analyzes diffs in merge requests and provides feedback.
-   **Configurable**: The plugin can be configured with different models, prompts, endpoints, max tokens, and Anthropic API versions.
-   **Secure**: API keys are handled as secrets.

## Configuration

The plugin is triggered by the command `!review` in a merge request.

The following variables can be used to configure the plugin:

| Name                               | Description                                                                 | Type          | Default Value                      |
| ---------------------------------- | --------------------------------------------------------------------------- | ------------- | ---------------------------------- |
| `claude_reviewer_api_key`          | Your Anthropic Claude API key.                                              | `env`, `secret` | (none)                             |
| `claude_reviewer_endpoint`         | The endpoint for the Claude API.                                            | `env`, `secret` | `https://api.anthropic.com/v1/messages` |
| `claude_reviewer_model`            | The Claude model to use for the review.                                     | `env`, `config` | `claude-3-5-sonnet-20240620`       |
| `claude_reviewer_prompt`           | A custom prompt to use for the review.                                      | `env`, `config` | (see code)                         |
| `claude_reviewer_max_tokens`       | The maximum number of tokens to generate in the response.                   | `env`, `config` | `1024`                             |
| `claude_reviewer_anthropic_version`| The Anthropic API version to use.                                           | `env`, `config` | `2023-06-01`                       |

## Usage

1.  Install the plugin in your merge-bot.
2.  Configure the required variables, especially `claude_reviewer_api_key`.
3.  Use the `!review` command in a merge request to trigger a review.

## WASM Artifact

The compiled WASM plugin is available at the URL specified in the `wasm_config.url` field of the `claude-reviewer.yaml` file.
