package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) History(ctx context.Context, query api.HistoryQuery) (api.HistoryPage, error) {
	values := url.Values{}
	if !query.Since.IsZero() {
		values.Set("since", query.Since.Format(time.RFC3339Nano))
	}
	if !query.Until.IsZero() {
		values.Set("until", query.Until.Format(time.RFC3339Nano))
	}
	if query.Device != "" {
		values.Set("device", string(query.Device))
	}
	if query.File != "" {
		values.Set("file", query.File)
	}
	if query.Kind != "" {
		values.Set("kind", query.Kind)
	}
	if query.Outcome != "" {
		values.Set("outcome", query.Outcome)
	}
	if query.RunID != "" {
		values.Set("run", query.RunID)
	}
	if query.Before > 0 {
		values.Set("before", strconv.FormatInt(query.Before, 10))
	}
	if query.After > 0 {
		values.Set("after", strconv.FormatInt(query.After, 10))
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	var page api.HistoryPage
	err := c.getJSON(ctx, "list-history", api.HistoryPath, values, &page)
	return page, err
}

func (c *Client) HistoryEntry(ctx context.Context, id int64) (api.HistoryEntry, error) {
	var entry api.HistoryEntry
	err := c.getJSON(ctx, "inspect-history", api.HistoryPath+"/"+strconv.FormatInt(id, 10), nil, &entry)
	return entry, err
}

func (c *Client) Automations(ctx context.Context) ([]api.AutomationFile, error) {
	var files []api.AutomationFile
	err := c.getJSON(ctx, "list-automations", api.AutomationsPath, nil, &files)
	return files, err
}

func (c *Client) SetAutomationPaused(ctx context.Context, file string, paused bool) (api.AutomationFile, error) {
	var result api.AutomationFile
	err := c.requestJSON(ctx, "pause-automation", http.MethodPut,
		api.AutomationsPath+"/"+url.PathEscape(file)+"/pause", nil,
		struct {
			Paused bool `json:"paused"`
		}{paused}, &result, c.timeout)
	return result, err
}
