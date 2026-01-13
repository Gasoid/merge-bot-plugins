{{ range .Versions }}
[Merge-Bot docs](https://github.com/Gasoid/merge-bot/blob/main/plugins.md)

### Installation

To use a plugin, you need to configure your Merge-Bot instance by setting the `PLUGINS` environment variable. This variable should point to the plugin's YAML configuration file.

For example, to install the **OpenAI Reviewer** plugin, you would set the following environment variables:

```bash
export PLUGINS="https://github.com/Gasoid/merge-bot-plugins/releases/download/{{ .Tag.Name }}/openai-reviewer.yaml"
export REVIEWER_API_KEY="your_openai_api_key"
```

Please note that each plugin has its own set of required environment variables for configuration (like API keys). For detailed installation and configuration instructions, please refer to the `README.md` file of the specific plugin you want to use.

{{ range .CommitGroups -}}
### {{ .Title }}

{{ range .Commits -}}
* {{ .Subject }}
{{ end }}
{{ end -}}

{{- if .RevertCommits -}}
### Reverts

{{ range .RevertCommits -}}
* {{ .Revert.Header }}
{{ end }}
{{ end -}}

{{- if .MergeCommits -}}
### Pull Requests

{{ range .MergeCommits -}}
* {{ .Header }}
{{ end }}
{{ end -}}

{{- if .NoteGroups -}}
{{ range .NoteGroups -}}
### {{ .Title }}

{{ range .Notes }}
{{ .Body }}
{{ end }}
{{ end -}}
{{ end -}}
{{ end -}}