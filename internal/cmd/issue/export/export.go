package export

import (
	"encoding/json"
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

	cmdutil.ExitIfError(os.MkdirAll(outputDir, dirPerm))

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

		var names map[string]string
		if !noAttachments && len(attachments) > 0 {
			names = deduplicateFilenames(attachments)
		}

		md := generateMarkdown(iss, attachments, names, server)

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

func generateMarkdown(iss *jira.Issue, attachments []jira.Attachment, names map[string]string, server string) string {
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
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(escaped)
	return fmt.Sprintf(`"%s"`, escaped)
}

func ifaceToADF(v interface{}) *adf.ADF {
	if v == nil {
		return nil
	}
	js, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var doc *adf.ADF
	if err := json.Unmarshal(js, &doc); err != nil {
		return nil
	}
	return doc
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
	if adfNode := ifaceToADF(desc); adfNode != nil {
		return adf.NewTranslator(adfNode, adf.NewMarkdownTranslator()).Translate()
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
		} else if c.Body != nil {
			if adfNode := ifaceToADF(c.Body); adfNode != nil {
				body = adf.NewTranslator(adfNode, adf.NewMarkdownTranslator()).Translate()
			}
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
		seen[name] = true
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
