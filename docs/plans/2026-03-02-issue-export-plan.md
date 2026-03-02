# Issue Export Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `jira issue export` command that exports Jira issues as markdown files with downloaded attachments.

**Architecture:** Pure API client layer in `pkg/jira/` for attachment metadata + content streaming. Export command in `internal/cmd/issue/export/` handles all orchestration, markdown generation, and file I/O. Proxy layer in `api/client.go` for v2/v3 switching.

**Tech Stack:** Go, cobra (CLI), spf13/viper (config), testify/assert (tests), `pkg/adf` + `pkg/md` (description conversion)

**Design doc:** `docs/plans/2026-03-02-issue-export-design.md`

---

### Task 1: Add Attachment type and field to IssueFields

**Files:**
- Modify: `pkg/jira/types.go:126-127` (add Attachment field after Updated)
- Modify: `pkg/jira/types.go:166` (add Attachment type after IssueLinkType)

**Step 1: Add the Attachment type after IssueLinkType (line 166)**

In `pkg/jira/types.go`, after the `IssueLinkType` struct (ends at line 166), add:

```go
// Attachment holds attachment info.
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

**Step 2: Add Attachment field to IssueFields**

In `pkg/jira/types.go`, after the `Updated` field (line 126), add:

```go
	Attachment []Attachment `json:"attachment"`
```

So lines 125-128 become:

```go
	Created    string       `json:"created"`
	Updated    string       `json:"updated"`
	Attachment []Attachment `json:"attachment"`
}
```

**Step 3: Verify existing tests still pass**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./pkg/jira/ -run TestGetIssue -v`
Expected: All existing tests pass (new field is zero-value when absent from JSON)

**Step 4: Commit**

```bash
git add pkg/jira/types.go
git commit -m "feat(jira): add Attachment type and field to IssueFields"
```

---

### Task 2: Add attachment API methods with tests

**Files:**
- Create: `pkg/jira/attachment.go`
- Create: `pkg/jira/attachment_test.go`
- Create: `pkg/jira/testdata/attachments.json`

**Step 1: Create test fixture file**

Create `pkg/jira/testdata/attachments.json`:

```json
{
  "key": "TEST-1",
  "fields": {
    "attachment": [
      {
        "id": "10001",
        "filename": "screenshot.png",
        "mimeType": "image/png",
        "size": 12345,
        "content": "http://localhost/attachment/content/10001",
        "created": "2024-01-15T10:30:00.000+0000",
        "author": {
          "accountId": "abc123",
          "displayName": "Test User"
        }
      },
      {
        "id": "10002",
        "filename": "document.pdf",
        "mimeType": "application/pdf",
        "size": 54321,
        "content": "http://localhost/attachment/content/10002",
        "created": "2024-01-16T14:20:00.000+0000",
        "author": {
          "accountId": "def456",
          "displayName": "Another User"
        }
      }
    ]
  }
}
```

**Step 2: Write failing tests**

Create `pkg/jira/attachment_test.go`:

```go
package jira

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetIssueAttachments(t *testing.T) {
	var unexpectedStatusCode bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/issue/TEST-1", r.URL.Path)
		assert.Equal(t, "attachment", r.URL.Query().Get("fields"))

		if unexpectedStatusCode {
			w.WriteHeader(400)
		} else {
			resp, err := os.ReadFile("./testdata/attachments.json")
			assert.NoError(t, err)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write(resp)
		}
	}))
	defer server.Close()

	client := NewClient(Config{Server: server.URL}, WithTimeout(3*time.Second))

	attachments, err := client.GetIssueAttachments("TEST-1")
	assert.NoError(t, err)
	assert.Len(t, attachments, 2)
	assert.Equal(t, "screenshot.png", attachments[0].Filename)
	assert.Equal(t, "document.pdf", attachments[1].Filename)
	assert.Equal(t, 12345, attachments[0].Size)

	unexpectedStatusCode = true

	_, err = client.GetIssueAttachments("TEST-1")
	assert.Error(t, &ErrUnexpectedResponse{}, err)
}

func TestGetIssueAttachmentsV2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/TEST-1", r.URL.Path)
		assert.Equal(t, "attachment", r.URL.Query().Get("fields"))

		resp, err := os.ReadFile("./testdata/attachments.json")
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write(resp)
	}))
	defer server.Close()

	client := NewClient(Config{Server: server.URL}, WithTimeout(3*time.Second))

	attachments, err := client.GetIssueAttachmentsV2("TEST-1")
	assert.NoError(t, err)
	assert.Len(t, attachments, 2)
}

func TestGetIssueAttachments_NoAttachments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"key": "TEST-1", "fields": {"attachment": []}}`))
	}))
	defer server.Close()

	client := NewClient(Config{Server: server.URL}, WithTimeout(3*time.Second))

	attachments, err := client.GetIssueAttachments("TEST-1")
	assert.NoError(t, err)
	assert.Len(t, attachments, 0)
}

func TestGetAttachmentContent(t *testing.T) {
	expectedContent := []byte("test file content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		_, _ = w.Write(expectedContent)
	}))
	defer server.Close()

	client := NewClient(Config{Server: server.URL}, WithTimeout(3*time.Second))

	body, err := client.GetAttachmentContent(server.URL + "/content")
	assert.NoError(t, err)
	defer func() { _ = body.Close() }()

	content, err := io.ReadAll(body)
	assert.NoError(t, err)
	assert.Equal(t, expectedContent, content)
}

func TestGetAttachmentContent_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := NewClient(Config{Server: server.URL}, WithTimeout(3*time.Second))

	_, err := client.GetAttachmentContent(server.URL + "/content")
	assert.Error(t, err)
}
```

**Step 3: Run tests to verify they fail**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./pkg/jira/ -run "TestGetIssueAttachments|TestGetAttachmentContent" -v`
Expected: FAIL — methods don't exist yet

**Step 4: Implement the API methods**

Create `pkg/jira/attachment.go`:

```go
package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetIssueAttachments fetches attachments for an issue using v3 API.
func (c *Client) GetIssueAttachments(key string) ([]Attachment, error) {
	return c.getIssueAttachments(key, apiVersion3)
}

// GetIssueAttachmentsV2 fetches attachments for an issue using v2 API.
func (c *Client) GetIssueAttachmentsV2(key string) ([]Attachment, error) {
	return c.getIssueAttachments(key, apiVersion2)
}

func (c *Client) getIssueAttachments(key, ver string) ([]Attachment, error) {
	path := fmt.Sprintf("/issue/%s?fields=attachment", key)

	var (
		res *http.Response
		err error
	)

	switch ver {
	case apiVersion2:
		res, err = c.GetV2(context.Background(), path, nil)
	default:
		res, err = c.Get(context.Background(), path, nil)
	}

	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, ErrEmptyResponse
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, formatUnexpectedResponse(res)
	}

	var issue Issue
	if err := json.NewDecoder(res.Body).Decode(&issue); err != nil {
		return nil, err
	}

	return issue.Fields.Attachment, nil
}

// GetAttachmentContent fetches attachment content from the given URL.
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) GetAttachmentContent(contentURL string) (io.ReadCloser, error) {
	res, err := c.request(context.Background(), http.MethodGet, contentURL, nil, nil)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, ErrEmptyResponse
	}

	if res.StatusCode != http.StatusOK {
		defer func() { _ = res.Body.Close() }()
		return nil, formatUnexpectedResponse(res)
	}

	return res.Body, nil
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./pkg/jira/ -run "TestGetIssueAttachments|TestGetAttachmentContent" -v`
Expected: PASS

**Step 6: Run full pkg/jira test suite to check for regressions**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./pkg/jira/ -v`
Expected: All tests pass

**Step 7: Commit**

```bash
git add pkg/jira/attachment.go pkg/jira/attachment_test.go pkg/jira/testdata/attachments.json
git commit -m "feat(jira): add attachment API methods with tests"
```

---

### Task 3: Add ProxyGetIssueAttachments to api/client.go

**Files:**
- Modify: `api/client.go:230` (append after ProxyWatchIssue)

**Step 1: Add the proxy function**

Append to `api/client.go` after line 230:

```go

// ProxyGetIssueAttachments uses either v2 or v3 version of the Jira API
// to fetch issue attachments based on configured installation type.
func ProxyGetIssueAttachments(c *jira.Client, key string) ([]jira.Attachment, error) {
	it := viper.GetString("installation")

	if it == jira.InstallationTypeLocal {
		return c.GetIssueAttachmentsV2(key)
	}
	return c.GetIssueAttachments(key)
}
```

**Step 2: Verify it compiles**

Run: `cd /home/wes/Devel/kh/jira-cli && go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add api/client.go
git commit -m "feat(api): add ProxyGetIssueAttachments proxy function"
```

---

### Task 4: Create the export command with markdown generation

This is the core task. The export command lives in `internal/cmd/issue/export/export.go`.

**Files:**
- Create: `internal/cmd/issue/export/export.go`
- Create: `internal/cmd/issue/export/export_test.go`

**Step 1: Write tests for markdown generation**

Create `internal/cmd/issue/export/export_test.go`. Focus on the pure `generateMarkdown` function
which takes issue data and attachment info and returns a string — no I/O, fully testable.

```go
package export

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestGenerateMarkdown_FullIssue(t *testing.T) {
	iss := &jira.Issue{
		Key: "ENG-1234",
		Fields: jira.IssueFields{
			Summary:     "Implement user authentication",
			Description: "This is the **description** in markdown.",
			Labels:      []string{"backend", "security"},
			IssueType:   jira.IssueType{Name: "Story"},
			Priority:    struct{ Name string `json:"name"` }{Name: "High"},
			Status:      struct{ Name string `json:"name"` }{Name: "In Progress"},
			Assignee:    struct{ Name string `json:"displayName"` }{Name: "Jane Smith"},
			Reporter:    struct{ Name string `json:"displayName"` }{Name: "John Doe"},
			Components:  []struct{ Name string `json:"name"` }{{Name: "API"}},
			FixVersions: []struct{ Name string `json:"name"` }{{Name: "v2.1"}},
			Created:     "2024-01-15T10:30:00+0000",
			Updated:     "2024-02-20T14:20:00+0000",
		},
	}

	attachments := []jira.Attachment{
		{ID: "10001", Filename: "screenshot.png", MimeType: "image/png"},
		{ID: "10002", Filename: "document.pdf", MimeType: "application/pdf"},
	}

	result := generateMarkdown(iss, attachments, "https://company.atlassian.net")

	// Check frontmatter
	assert.Contains(t, result, "key: ENG-1234")
	assert.Contains(t, result, "summary: Implement user authentication")
	assert.Contains(t, result, "type: Story")
	assert.Contains(t, result, "status: In Progress")
	assert.Contains(t, result, "priority: High")
	assert.Contains(t, result, "assignee: Jane Smith")
	assert.Contains(t, result, "reporter: John Doe")
	assert.Contains(t, result, "labels: [backend, security]")
	assert.Contains(t, result, "components: [API]")
	assert.Contains(t, result, "fix_versions: [v2.1]")
	assert.Contains(t, result, "url: https://company.atlassian.net/browse/ENG-1234")

	// Check heading
	assert.Contains(t, result, "# Implement user authentication")

	// Check description
	assert.Contains(t, result, "## Description")
	assert.Contains(t, result, "This is the **description** in markdown.")

	// Check attachments section
	assert.Contains(t, result, "## Attachments")
	assert.Contains(t, result, "![screenshot.png](attachments/ENG-1234/screenshot.png)")
	assert.Contains(t, result, "[document.pdf](attachments/ENG-1234/document.pdf)")
}

func TestGenerateMarkdown_NoDescription(t *testing.T) {
	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "No desc issue",
			IssueType: jira.IssueType{Name: "Bug"},
			Status:    struct{ Name string `json:"name"` }{Name: "To Do"},
			Created:   "2024-01-15T10:30:00+0000",
			Updated:   "2024-01-15T10:30:00+0000",
		},
	}

	result := generateMarkdown(iss, nil, "https://example.com")

	assert.NotContains(t, result, "## Description")
	assert.NotContains(t, result, "## Attachments")
}

func TestGenerateMarkdown_WithSubtasks(t *testing.T) {
	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "Parent issue",
			IssueType: jira.IssueType{Name: "Story"},
			Status:    struct{ Name string `json:"name"` }{Name: "In Progress"},
			Created:   "2024-01-15T10:30:00+0000",
			Updated:   "2024-01-15T10:30:00+0000",
			Subtasks: []jira.Issue{
				{
					Key: "ENG-2",
					Fields: jira.IssueFields{
						Summary:  "Subtask one",
						Priority: struct{ Name string `json:"name"` }{Name: "High"},
						Status:   struct{ Name string `json:"name"` }{Name: "Done"},
					},
				},
			},
		},
	}

	result := generateMarkdown(iss, nil, "https://example.com")

	assert.Contains(t, result, "## Subtasks")
	assert.Contains(t, result, "| ENG-2 | Subtask one | High | Done |")
}

func TestGenerateMarkdown_WithComments(t *testing.T) {
	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "Issue with comments",
			IssueType: jira.IssueType{Name: "Bug"},
			Status:    struct{ Name string `json:"name"` }{Name: "To Do"},
			Created:   "2024-01-15T10:30:00+0000",
			Updated:   "2024-01-15T10:30:00+0000",
			Comment: struct {
				Comments []struct {
					ID      string      `json:"id"`
					Author  jira.User   `json:"author"`
					Body    interface{} `json:"body"`
					Created string      `json:"created"`
				} `json:"comments"`
				Total int `json:"total"`
			}{
				Comments: []struct {
					ID      string      `json:"id"`
					Author  jira.User   `json:"author"`
					Body    interface{} `json:"body"`
					Created string      `json:"created"`
				}{
					{
						ID:      "1",
						Author:  jira.User{DisplayName: "Jane Smith"},
						Body:    "This is a comment.",
						Created: "2024-01-16T09:00:00+0000",
					},
				},
				Total: 1,
			},
		},
	}

	result := generateMarkdown(iss, nil, "https://example.com")

	assert.Contains(t, result, "## Comments")
	assert.Contains(t, result, "### Jane Smith")
	assert.Contains(t, result, "This is a comment.")
}

func TestGenerateMarkdown_WithLinkedIssues(t *testing.T) {
	outwardIssue := &jira.Issue{
		Key: "ENG-5",
		Fields: jira.IssueFields{
			Summary:   "Blocked issue",
			IssueType: jira.IssueType{Name: "Task"},
			Priority:  struct{ Name string `json:"name"` }{Name: "High"},
			Status:    struct{ Name string `json:"name"` }{Name: "To Do"},
		},
	}

	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "Issue with links",
			IssueType: jira.IssueType{Name: "Story"},
			Status:    struct{ Name string `json:"name"` }{Name: "In Progress"},
			Created:   "2024-01-15T10:30:00+0000",
			Updated:   "2024-01-15T10:30:00+0000",
			IssueLinks: []struct {
				ID       string `json:"id"`
				LinkType struct {
					Name    string `json:"name"`
					Inward  string `json:"inward"`
					Outward string `json:"outward"`
				} `json:"type"`
				InwardIssue  *jira.Issue `json:"inwardIssue,omitempty"`
				OutwardIssue *jira.Issue `json:"outwardIssue,omitempty"`
			}{
				{
					ID: "100",
					LinkType: struct {
						Name    string `json:"name"`
						Inward  string `json:"inward"`
						Outward string `json:"outward"`
					}{Name: "Blocks", Inward: "is blocked by", Outward: "blocks"},
					OutwardIssue: outwardIssue,
				},
			},
		},
	}

	result := generateMarkdown(iss, nil, "https://example.com")

	assert.Contains(t, result, "## Linked Issues")
	assert.Contains(t, result, "**blocks**")
	assert.Contains(t, result, "| ENG-5 | Blocked issue | Task | High | To Do |")
}

func TestIsImage(t *testing.T) {
	assert.True(t, isImage("photo.png"))
	assert.True(t, isImage("photo.PNG"))
	assert.True(t, isImage("photo.jpg"))
	assert.True(t, isImage("photo.jpeg"))
	assert.True(t, isImage("photo.gif"))
	assert.True(t, isImage("photo.svg"))
	assert.True(t, isImage("photo.webp"))
	assert.True(t, isImage("photo.bmp"))
	assert.False(t, isImage("doc.pdf"))
	assert.False(t, isImage("data.csv"))
	assert.False(t, isImage("archive.zip"))
}

func TestSafeFilename(t *testing.T) {
	assert.Equal(t, "file.txt", safeFilename("file.txt", "123"))
	assert.Equal(t, "file.txt", safeFilename("../../../file.txt", "123"))
	assert.Equal(t, "file.txt", safeFilename("/etc/file.txt", "123"))
	assert.Equal(t, "123", safeFilename(".", "123"))
	assert.Equal(t, "123", safeFilename("/", "123"))
	assert.Equal(t, "123", safeFilename("", "123"))
}

func TestDeduplicateFilenames(t *testing.T) {
	attachments := []jira.Attachment{
		{ID: "100", Filename: "screenshot.png"},
		{ID: "101", Filename: "screenshot.png"},
		{ID: "102", Filename: "document.pdf"},
	}

	names := deduplicateFilenames(attachments)

	assert.Equal(t, "screenshot.png", names["100"])
	assert.Equal(t, "screenshot-101.png", names["101"])
	assert.Equal(t, "document.pdf", names["102"])
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "500 B", formatSize(500))
	assert.Equal(t, "1.5 KB", formatSize(1536))
	assert.Equal(t, "2.0 MB", formatSize(2*1024*1024))
	assert.Equal(t, "1.0 GB", formatSize(1024*1024*1024))
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./internal/cmd/issue/export/ -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Implement the export command**

Create `internal/cmd/issue/export/export.go`:

```go
package export

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/pkg/adf"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	jiraMD "github.com/ankitpokhrel/jira-cli/pkg/md"
)

const (
	helpText = `Export exports issues as markdown files with attachments.`
	examples = `$ jira issue export ISSUE-1

# Export multiple issues
$ jira issue export ISSUE-1 ISSUE-2 ISSUE-3

# Export to a custom directory
$ jira issue export ISSUE-1 -o ./my-exports

# Export without downloading attachments
$ jira issue export ISSUE-1 --no-attachments`

	defaultOutputDir = "./docs/jira-issue"
	dirPerm          = 0o750
	filePerm         = 0o640
)

// NewCmdExport is an export command.
func NewCmdExport() *cobra.Command {
	cmd := cobra.Command{
		Use:     "export ISSUE-KEY [ISSUE-KEY...]",
		Short:   "Export issues as markdown files with attachments",
		Long:    helpText,
		Example: examples,
		Aliases: []string{"exp"},
		Annotations: map[string]string{
			"help:args": "ISSUE-KEY\tIssue key, eg: ISSUE-1 (accepts multiple)",
		},
		Args: cobra.MinimumNArgs(1),
		Run:  export,
	}

	cmd.Flags().StringP("output-dir", "o", defaultOutputDir, "Output directory")
	cmd.Flags().Bool("no-attachments", false, "Skip downloading attachments")

	return &cmd
}

func export(cmd *cobra.Command, args []string) {
	debug, err := cmd.Flags().GetBool("debug")
	cmdutil.ExitIfError(err)

	outputDir, err := cmd.Flags().GetString("output-dir")
	cmdutil.ExitIfError(err)

	noAttachments, err := cmd.Flags().GetBool("no-attachments")
	cmdutil.ExitIfError(err)

	server := viper.GetString("server")
	project := viper.GetString("project.key")
	client := api.DefaultClient(debug)

	var (
		exported   int
		downloaded int
		failed     int
	)

	for _, arg := range args {
		key := cmdutil.GetJiraIssueKey(project, arg)

		iss, err := func() (*jira.Issue, error) {
			s := cmdutil.Info(fmt.Sprintf("Fetching %s...", key))
			defer s.Stop()
			return api.ProxyGetIssue(client, key)
		}()
		if err != nil {
			cmdutil.Fail("Failed to fetch %s: %v", key, err)
			continue
		}

		attachments := iss.Fields.Attachment

		md := generateMarkdown(iss, attachments, server)

		if err := os.MkdirAll(outputDir, dirPerm); err != nil {
			cmdutil.ExitIfError(fmt.Errorf("failed to create directory %s: %w", outputDir, err))
		}

		mdPath := filepath.Join(outputDir, key+".md")
		if err := os.WriteFile(mdPath, []byte(md), filePerm); err != nil {
			cmdutil.Fail("Failed to write %s: %v", mdPath, err)
			continue
		}

		exported++

		if noAttachments || len(attachments) == 0 {
			cmdutil.Success("Exported %s", key)
			continue
		}

		names := deduplicateFilenames(attachments)

		for _, att := range attachments {
			name := names[att.ID]
			attDir := filepath.Join(outputDir, "attachments", key)

			if err := os.MkdirAll(attDir, dirPerm); err != nil {
				cmdutil.Fail("Failed to create directory %s: %v", attDir, err)
				failed++
				continue
			}

			targetPath := filepath.Join(attDir, name)

			err := func() error {
				s := cmdutil.Info(fmt.Sprintf("Downloading %s (%s)", name, formatSize(att.Size)))
				defer s.Stop()

				body, err := client.GetAttachmentContent(att.Content)
				if err != nil {
					return err
				}
				defer func() { _ = body.Close() }()

				out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
				if err != nil {
					return fmt.Errorf("failed to create file: %w", err)
				}
				defer func() { _ = out.Close() }()

				if _, err := io.Copy(out, body); err != nil {
					return fmt.Errorf("failed to write file: %w", err)
				}
				return nil
			}()

			if err != nil {
				cmdutil.Fail("Failed to download %s: %v", name, err)
				failed++
			} else {
				downloaded++
			}
		}

		cmdutil.Success("Exported %s", key)
	}

	fmt.Println()
	if failed > 0 {
		cmdutil.Warn("Exported %d issues to %s (%d attachments downloaded, %d failed)", exported, outputDir, downloaded, failed)
	} else if downloaded > 0 {
		cmdutil.Success("Exported %d issues to %s (%d attachments downloaded)", exported, outputDir, downloaded)
	} else {
		cmdutil.Success("Exported %d issues to %s", exported, outputDir)
	}
}

func generateMarkdown(iss *jira.Issue, attachments []jira.Attachment, server string) string {
	var buf strings.Builder

	writeFrontmatter(&buf, iss, server)
	buf.WriteString(fmt.Sprintf("# %s\n", iss.Fields.Summary))

	if desc := descriptionToMarkdown(iss.Fields.Description); desc != "" {
		buf.WriteString(fmt.Sprintf("\n## Description\n\n%s\n", desc))
	}

	if len(iss.Fields.Subtasks) > 0 {
		writeSubtasks(&buf, iss.Fields.Subtasks)
	}

	if len(iss.Fields.IssueLinks) > 0 {
		writeLinkedIssues(&buf, iss.Fields.IssueLinks)
	}

	if iss.Fields.Comment.Total > 0 {
		writeComments(&buf, iss)
	}

	if len(attachments) > 0 {
		names := deduplicateFilenames(attachments)
		writeAttachments(&buf, iss.Key, attachments, names)
	}

	return buf.String()
}

func writeFrontmatter(buf *strings.Builder, iss *jira.Issue, server string) {
	buf.WriteString("---\n")
	buf.WriteString(fmt.Sprintf("key: %s\n", iss.Key))
	buf.WriteString(fmt.Sprintf("summary: %s\n", yamlEscape(iss.Fields.Summary)))
	buf.WriteString(fmt.Sprintf("type: %s\n", iss.Fields.IssueType.Name))
	buf.WriteString(fmt.Sprintf("status: %s\n", iss.Fields.Status.Name))

	if iss.Fields.Priority.Name != "" {
		buf.WriteString(fmt.Sprintf("priority: %s\n", iss.Fields.Priority.Name))
	}
	if iss.Fields.Assignee.Name != "" {
		buf.WriteString(fmt.Sprintf("assignee: %s\n", iss.Fields.Assignee.Name))
	}
	if iss.Fields.Reporter.Name != "" {
		buf.WriteString(fmt.Sprintf("reporter: %s\n", iss.Fields.Reporter.Name))
	}
	if len(iss.Fields.Labels) > 0 {
		buf.WriteString(fmt.Sprintf("labels: [%s]\n", strings.Join(iss.Fields.Labels, ", ")))
	}
	if len(iss.Fields.Components) > 0 {
		names := make([]string, 0, len(iss.Fields.Components))
		for _, c := range iss.Fields.Components {
			names = append(names, c.Name)
		}
		buf.WriteString(fmt.Sprintf("components: [%s]\n", strings.Join(names, ", ")))
	}
	if len(iss.Fields.FixVersions) > 0 {
		names := make([]string, 0, len(iss.Fields.FixVersions))
		for _, v := range iss.Fields.FixVersions {
			names = append(names, v.Name)
		}
		buf.WriteString(fmt.Sprintf("fix_versions: [%s]\n", strings.Join(names, ", ")))
	}
	if iss.Fields.Created != "" {
		buf.WriteString(fmt.Sprintf("created: \"%s\"\n", iss.Fields.Created))
	}
	if iss.Fields.Updated != "" {
		buf.WriteString(fmt.Sprintf("updated: \"%s\"\n", iss.Fields.Updated))
	}

	url := cmdutil.GenerateServerBrowseURL(server, iss.Key)
	buf.WriteString(fmt.Sprintf("url: %s\n", url))

	buf.WriteString("---\n\n")
}

func yamlEscape(s string) string {
	if strings.ContainsAny(s, ":{}[]&*?|>!%#`@,") || strings.HasPrefix(s, "'") || strings.HasPrefix(s, "\"") {
		escaped := strings.ReplaceAll(s, "\"", "\\\"")
		return fmt.Sprintf("\"%s\"", escaped)
	}
	return s
}

func descriptionToMarkdown(desc interface{}) string {
	if desc == nil {
		return ""
	}
	if adfNode, ok := desc.(*adf.ADF); ok {
		return adf.NewTranslator(adfNode, adf.NewMarkdownTranslator()).Translate()
	}
	if s, ok := desc.(string); ok {
		if s == "" {
			return ""
		}
		return jiraMD.FromJiraMD(s)
	}
	return ""
}

func writeSubtasks(buf *strings.Builder, subtasks []jira.Issue) {
	buf.WriteString("\n## Subtasks\n\n")
	buf.WriteString("| Key | Summary | Priority | Status |\n")
	buf.WriteString("|-----|---------|----------|--------|\n")
	for _, task := range subtasks {
		buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			task.Key, task.Fields.Summary, task.Fields.Priority.Name, task.Fields.Status.Name))
	}
}

func writeLinkedIssues(buf *strings.Builder, links []struct {
	ID       string `json:"id"`
	LinkType struct {
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *jira.Issue `json:"inwardIssue,omitempty"`
	OutwardIssue *jira.Issue `json:"outwardIssue,omitempty"`
}) {
	linkMap := make(map[string][]*jira.Issue)
	keys := make([]string, 0)

	for _, link := range links {
		var (
			linkType    string
			linkedIssue *jira.Issue
		)

		if link.InwardIssue != nil {
			linkType = link.LinkType.Inward
			linkedIssue = link.InwardIssue
		} else if link.OutwardIssue != nil {
			linkType = link.LinkType.Outward
			linkedIssue = link.OutwardIssue
		}

		if linkedIssue == nil || linkedIssue.Key == "" {
			continue
		}

		if _, ok := linkMap[linkType]; !ok {
			keys = append(keys, linkType)
		}
		linkMap[linkType] = append(linkMap[linkType], linkedIssue)
	}

	if len(linkMap) == 0 {
		return
	}

	buf.WriteString("\n## Linked Issues\n")

	sort.Strings(keys)

	for _, k := range keys {
		buf.WriteString(fmt.Sprintf("\n**%s**\n\n", k))
		buf.WriteString("| Key | Summary | Type | Priority | Status |\n")
		buf.WriteString("|-----|---------|------|----------|--------|\n")
		for _, iss := range linkMap[k] {
			buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				iss.Key, iss.Fields.Summary, iss.Fields.IssueType.Name,
				iss.Fields.Priority.Name, iss.Fields.Status.Name))
		}
	}
}

func writeComments(buf *strings.Builder, iss *jira.Issue) {
	buf.WriteString("\n## Comments\n")

	for _, c := range iss.Fields.Comment.Comments {
		author := c.Author.DisplayName
		if author == "" {
			author = c.Author.Name
		}
		date := cmdutil.FormatDateTimeHuman(c.Created, jira.RFC3339)

		buf.WriteString(fmt.Sprintf("\n### %s — %s\n\n", author, date))

		var body string
		if adfNode, ok := c.Body.(*adf.ADF); ok {
			body = adf.NewTranslator(adfNode, adf.NewMarkdownTranslator()).Translate()
		} else if s, ok := c.Body.(string); ok {
			body = jiraMD.FromJiraMD(s)
		}
		buf.WriteString(body + "\n")
	}
}

func writeAttachments(buf *strings.Builder, key string, attachments []jira.Attachment, names map[string]string) {
	buf.WriteString("\n## Attachments\n\n")
	for _, att := range attachments {
		name := names[att.ID]
		relPath := fmt.Sprintf("attachments/%s/%s", key, name)
		if isImage(name) {
			buf.WriteString(fmt.Sprintf("![%s](%s)\n\n", name, relPath))
		} else {
			buf.WriteString(fmt.Sprintf("[%s](%s)\n\n", name, relPath))
		}
	}
}

func isImage(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp":
		return true
	}
	return false
}

func safeFilename(filename, fallbackID string) string {
	name := filepath.Base(filename)
	if name == "." || name == "/" || name == "" {
		return fallbackID
	}
	return name
}

func deduplicateFilenames(attachments []jira.Attachment) map[string]string {
	result := make(map[string]string, len(attachments))
	seen := make(map[string]bool)

	for _, att := range attachments {
		name := safeFilename(att.Filename, att.ID)
		if seen[name] {
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s-%s%s", base, att.ID, ext)
		}
		seen[safeFilename(att.Filename, att.ID)] = true
		result[att.ID] = name
	}

	return result
}

func formatSize(bytes int) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./internal/cmd/issue/export/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cmd/issue/export/
git commit -m "feat(cmd): add issue export command with markdown generation"
```

---

### Task 5: Register the export command

**Files:**
- Modify: `internal/cmd/issue/issue.go:6-18` (add import)
- Modify: `internal/cmd/issue/issue.go:37-41` (add to AddCommand)

**Step 1: Add the import**

In `internal/cmd/issue/issue.go`, add to the import block (after the `assign` import, line 6):

```go
	"github.com/ankitpokhrel/jira-cli/internal/cmd/issue/export"
```

**Step 2: Register the command**

In `internal/cmd/issue/issue.go`, modify line 40 to add `export.NewCmdExport()`:

Change:
```go
		delete.NewCmdDelete(), watch.NewCmdWatch(), worklog.NewCmdWorklog(),
```

To:
```go
		delete.NewCmdDelete(), watch.NewCmdWatch(), worklog.NewCmdWorklog(), export.NewCmdExport(),
```

**Step 3: Verify it compiles**

Run: `cd /home/wes/Devel/kh/jira-cli && go build ./...`
Expected: Success

**Step 4: Run all tests**

Run: `cd /home/wes/Devel/kh/jira-cli && go test ./... 2>&1 | tail -30`
Expected: All tests pass

**Step 5: Commit**

```bash
git add internal/cmd/issue/issue.go
git commit -m "feat(cmd): register export command under issue"
```

---

### Task 6: Verify end-to-end with help output

**Step 1: Build and verify help**

Run: `cd /home/wes/Devel/kh/jira-cli && go build -o ./bin/jira ./... && ./bin/jira issue export --help`

Expected output should show:
- Usage: `export ISSUE-KEY [ISSUE-KEY...]`
- Flags: `--output-dir`, `--no-attachments`
- Examples section

**Step 2: Verify issue subcommand lists export**

Run: `./bin/jira issue --help`

Expected: `export` appears in the list of available commands
