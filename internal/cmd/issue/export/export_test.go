package export

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ankitpokhrel/jira-cli/pkg/adf"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestGenerateMarkdown_FullIssue(t *testing.T) {
	iss := &jira.Issue{
		Key: "ENG-1234",
		Fields: jira.IssueFields{
			Summary:     "Implement user authentication",
			Description: "This is the description in plain text.",
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

	names := deduplicateFilenames(attachments)
	result := generateMarkdown(iss, attachments, names, "https://company.atlassian.net")

	// Check frontmatter
	assert.Contains(t, result, "key: ENG-1234")
	assert.Contains(t, result, `summary: "Implement user authentication"`)
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
	assert.Contains(t, result, "This is the description in plain text.")

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

	result := generateMarkdown(iss, nil, nil, "https://example.com")

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

	result := generateMarkdown(iss, nil, nil, "https://example.com")

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

	result := generateMarkdown(iss, nil, nil, "https://example.com")

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

	result := generateMarkdown(iss, nil, nil, "https://example.com")

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

	// Test that dedup'd names don't collide with real filenames
	attachments2 := []jira.Attachment{
		{ID: "100", Filename: "screenshot.png"},
		{ID: "101", Filename: "screenshot.png"},
		{ID: "102", Filename: "screenshot-101.png"},
	}

	names2 := deduplicateFilenames(attachments2)
	// All three should get distinct names
	allNames := make(map[string]bool)
	for _, n := range names2 {
		allNames[n] = true
	}
	assert.Equal(t, 3, len(allNames), "all filenames should be unique")
}

func TestDeduplicateFilenames_RenameCollision(t *testing.T) {
	// The organic name "file-101.png" appears BEFORE the duplicate that would
	// be renamed to "file-101.png".  The rename must detect this collision.
	attachments := []jira.Attachment{
		{ID: "100", Filename: "file.png"},
		{ID: "102", Filename: "file-101.png"}, // organic name processed second
		{ID: "101", Filename: "file.png"},      // duplicate renamed to file-101.png → collides!
	}

	names := deduplicateFilenames(attachments)

	// All three must be unique
	seen := make(map[string]bool)
	for _, n := range names {
		assert.False(t, seen[n], "duplicate filename: %s", n)
		seen[n] = true
	}
	assert.Equal(t, 3, len(seen))

	// First file keeps original name
	assert.Equal(t, "file.png", names["100"])
	// Second file keeps its organic name
	assert.Equal(t, "file-101.png", names["102"])
	// Third file: its dedup'd name (file-101.png) collides with the organic name,
	// so it must get a different name
	assert.NotEqual(t, "file-101.png", names["101"], "should not collide with organic name from ID 102")
}

func TestWriteComments_ADFBody(t *testing.T) {
	// Simulate what happens when GetIssue returns comment bodies as raw
	// map[string]interface{} (the common case when no comment limit filter is passed).
	rawADFBody := map[string]interface{}{
		"version": float64(1),
		"type":    "doc",
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "This is the comment text.",
					},
				},
			},
		},
	}

	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "Issue with ADF comments",
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
						Body:    rawADFBody,
						Created: "2024-01-16T09:00:00+0000",
					},
				},
				Total: 1,
			},
		},
	}

	result := generateMarkdown(iss, nil, nil, "https://example.com")

	assert.Contains(t, result, "## Comments")
	assert.Contains(t, result, "### Jane Smith")
	assert.Contains(t, result, "This is the comment text.")
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "500 B", formatSize(500))
	assert.Equal(t, "1.5 KB", formatSize(1536))
	assert.Equal(t, "2.0 MB", formatSize(2*1024*1024))
	assert.Equal(t, "1.0 GB", formatSize(1024*1024*1024))
}

func TestGenerateMarkdown_AttachmentNamesAlwaysResolved(t *testing.T) {
	// Even when names would normally be nil (--no-attachments mode),
	// generateMarkdown should still render filenames in the Attachments section.
	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:   "Issue",
			IssueType: jira.IssueType{Name: "Bug"},
			Status:    struct{ Name string `json:"name"` }{Name: "To Do"},
			Created:   "2024-01-15T10:30:00+0000",
			Updated:   "2024-01-15T10:30:00+0000",
		},
	}

	attachments := []jira.Attachment{
		{ID: "100", Filename: "screenshot.png"},
	}

	// Pass nil names map (simulating --no-attachments where names was not computed)
	result := generateMarkdown(iss, attachments, nil, "https://example.com")

	// Should still show the filename, not an empty link
	assert.Contains(t, result, "screenshot.png")
	assert.NotContains(t, result, "![](")
}

func TestGenerateMarkdown_InlineMedia(t *testing.T) {
	descADF := &adf.ADF{
		Version: 1,
		DocType: "doc",
		Content: []*adf.Node{
			{
				NodeType: adf.NodeParagraph,
				Content: []*adf.Node{
					{
						NodeType: adf.ChildNodeText,
						NodeValue: adf.NodeValue{
							Text: "See the screenshot below:",
						},
					},
				},
			},
			{
				NodeType: adf.NodeMedia,
				Attributes: map[string]any{
					"id":         "abc-123",
					"type":       "file",
					"collection": "some-collection",
				},
			},
		},
	}

	iss := &jira.Issue{
		Key: "ENG-1",
		Fields: jira.IssueFields{
			Summary:     "Issue with inline media",
			Description: descADF,
			IssueType:   jira.IssueType{Name: "Bug"},
			Status:      struct{ Name string `json:"name"` }{Name: "To Do"},
			Created:     "2024-01-15T10:30:00+0000",
			Updated:     "2024-01-15T10:30:00+0000",
		},
	}

	attachments := []jira.Attachment{
		{ID: "abc-123", Filename: "screenshot.png", MimeType: "image/png"},
		{ID: "def-456", Filename: "document.pdf", MimeType: "application/pdf"},
	}

	names := deduplicateFilenames(attachments)
	result := generateMarkdown(iss, attachments, names, "https://example.com")

	assert.Contains(t, result, "![screenshot.png](attachments/ENG-1/screenshot.png)")
	assert.NotContains(t, result, "[attachment]")
}
