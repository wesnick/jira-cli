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
