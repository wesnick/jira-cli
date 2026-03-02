# Issue Export Command Design

## Command Interface

```
jira issue export ISSUE-1 [ISSUE-2 ...] [flags]

Flags:
  -o, --output-dir string   Output directory (default: ./docs/jira-issue/)
      --no-attachments       Skip downloading attachments

Aliases: export, exp
```

Accepts one or more issue keys. Each issue produces one markdown file and its
attachments in a subdirectory.

## Output Structure

```
docs/jira-issue/
├── ENG-1234.md
├── ENG-5678.md
└── attachments/
    ├── ENG-1234/
    │   ├── screenshot.png
    │   └── document.pdf
    └── ENG-5678/
        ├── report.xlsx
        └── diagram.png
```

Markdown references use relative paths: `attachments/ENG-1234/screenshot.png`.

## Markdown Format

YAML frontmatter for metadata, then content sections. Sections with no data are
omitted entirely.

```markdown
---
key: ENG-1234
summary: Implement user authentication
type: Story
status: In Progress
priority: High
assignee: Jane Smith
reporter: John Doe
labels: [backend, security]
components: [API]
fix_versions: [v2.1]
created: "2024-01-15T10:30:00-0700"
updated: "2024-02-20T14:20:00-0700"
url: https://company.atlassian.net/browse/ENG-1234
---

# Implement user authentication

## Description

Markdown-converted description body...

## Subtasks

| Key | Summary | Priority | Status |
|-----|---------|----------|--------|
| ENG-1235 | Add login endpoint | High | Done |

## Linked Issues

**blocks**
| Key | Summary | Type | Priority | Status |
|-----|---------|------|----------|--------|
| ENG-1240 | Deploy auth service | Task | High | To Do |

## Comments

### Jane Smith — Mon, 18 Jan 24

Comment body in markdown...

## Attachments

![screenshot.png](attachments/ENG-1234/screenshot.png)

[document.pdf](attachments/ENG-1234/document.pdf)
```

Image files (png, jpg, jpeg, gif, svg, webp, bmp) use `![](...)` syntax.
All other files use `[filename](...)` link syntax.

## Architecture

### File Layout

```
pkg/jira/
├── types.go              Add Attachment type + field on IssueFields
├── attachment.go          GetIssueAttachments, GetIssueAttachmentsV2,
│                          GetAttachmentContent (returns io.ReadCloser)

api/
├── client.go              Add ProxyGetIssueAttachments

internal/cmd/issue/
├── issue.go               Register export subcommand
├── export/
│   └── export.go          Command definition, orchestration, markdown
│                           generation, file writing
```

### Responsibilities

- `pkg/jira/attachment.go` — Pure API client. Fetches attachment metadata and
  streams content. No disk I/O.
- `api/client.go` — Thin proxy for v2/v3 version switching.
- `internal/cmd/issue/export/export.go` — All orchestration: fetch issue, fetch
  attachments, generate markdown, write files, sanitize filenames, progress
  display.

### Data Flow

1. For each issue key: fetch issue via `api.ProxyGetIssue` to get `*jira.Issue`
2. Attachment metadata comes from the issue response (Attachment field on
   IssueFields, no extra API call needed)
3. Generate markdown string (frontmatter + body sections)
4. Write `.md` file to output dir
5. Unless `--no-attachments`: for each attachment, stream content via
   `client.GetAttachmentContent` (returns `io.ReadCloser`), write to
   `attachments/<KEY>/<sanitized-filename>`

## Attachment Handling

### Type Definition (pkg/jira/types.go)

```go
type Attachment struct {
    ID       string `json:"id"`
    Filename string `json:"filename"`
    MimeType string `json:"mimeType"`
    Size     int    `json:"size"`
    Content  string `json:"content"`
    Created  string `json:"created"`
    Author   User   `json:"author"`
}
```

Added to IssueFields: `Attachment []Attachment \`json:"attachment"\``

### Content Streaming (pkg/jira/attachment.go)

```go
func (c *Client) GetAttachmentContent(contentURL string) (io.ReadCloser, error)
```

Uses `c.request()` directly with the full content URL (absolute URL from the
Jira API, not a versioned API path). Returns the response body for the caller
to write to disk and close.

Also provides `GetIssueAttachments` / `GetIssueAttachmentsV2` for standalone
attachment fetching (useful outside the export command).

### Security — Filename Sanitization

```go
safeName := filepath.Base(att.Filename)
if safeName == "." || safeName == "/" {
    safeName = att.ID
}
targetPath := filepath.Join(outputDir, "attachments", key, safeName)
```

Path traversal protection via `filepath.Base()` — strips all directory
components from server-supplied filenames.

### Duplicate Filename Handling

Jira allows multiple attachments with the same filename. Deduplicate by
appending the attachment ID:

```
screenshot.png       → screenshot.png
screenshot.png (dup) → screenshot-10042.png
```

### Image Detection

```go
func isImage(filename string) bool {
    switch strings.ToLower(filepath.Ext(filename)) {
    case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp":
        return true
    }
    return false
}
```

## Error Handling

- **Partial attachment failures:** Continue downloading remaining attachments.
  Report summary at the end.
- **Issue fetch failure:** Print error, continue to next issue key.
- **Existing files:** Overwrite silently. Export is idempotent.
- **Empty description:** Omit Description section.
- **No attachments:** Omit Attachments section. Don't create the attachment
  subdirectory.

## Progress Feedback

Spinner per issue fetch and per attachment download using the standard
`cmdutil.Info` + `defer s.Stop()` pattern. Final summary:

```
✓ Exported 3 issues to docs/jira-issue/ (12 attachments downloaded, 1 failed)
```
