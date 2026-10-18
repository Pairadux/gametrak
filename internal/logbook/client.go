// Package logbook syncs the session log with a sheet on a Logbook site: it
// uploads sessions recorded here and mirrors the sheet back, so edits made on
// the site win.
package logbook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pageSize is the most rows Logbook returns per page.
const pageSize = 200

// Client talks to one sheet of a Logbook site's API.
type Client struct {
	baseURL string
	sheet   string
	creds   Credentials
	http    *http.Client
}

// NewClient returns a client for sheet on the site at baseURL.
func NewClient(baseURL, sheet string, creds Credentials) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		sheet:   sheet,
		creds:   creds,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// Cloudflare Access answers a request it won't let through with a
			// redirect to its login page; following it would read that page as
			// a successful response.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Target names the site and sheet, e.g. "https://logs.example.com (sheet gaming)".
func (c *Client) Target() string {
	return fmt.Sprintf("%s (sheet %s)", c.baseURL, c.sheet)
}

// Row is a row of the sheet as the API returns it.
type Row struct {
	ID         string  `json:"id"`
	OccurredAt string  `json:"occurred_at"`
	Data       RowData `json:"data"`
}

// RowData is a row's stored values.
type RowData struct {
	Game     string `json:"game"`
	Minutes  int    `json:"minutes"`
	Platform string `json:"platform"`
}

// NewRow is the body of a create request. OccurredAt is when the session started.
type NewRow struct {
	OccurredAt string `json:"occurred_at"`
	Game       string `json:"game"`
	Minutes    int    `json:"minutes"`
	Platform   string `json:"platform"`
}

// APIError is a response other than success, from Logbook or from Cloudflare
// Access in front of it.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("Logbook returned HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("Logbook returned HTTP %d (%s): %s", e.Status, e.Code, e.Message)
}

// Temporary reports whether err is worth retrying later unchanged: the network
// or the site being down, rather than a rejected token or request.
func Temporary(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500 || apiErr.Status == http.StatusTooManyRequests
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded)
}

// Version returns the sheet's change counter, which goes up on every write.
func (c *Client) Version(ctx context.Context) (int64, error) {
	var body struct {
		Version int64 `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, c.sheetURL("version", nil), nil, &body)
	return body.Version, err
}

// Rows returns every row of the sheet, oldest first.
func (c *Client) Rows(ctx context.Context) ([]Row, error) {
	var rows []Row
	cursor := ""
	for {
		query := url.Values{"sort": {"asc"}, "limit": {fmt.Sprint(pageSize)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page struct {
			Rows       []Row   `json:"rows"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := c.do(ctx, http.MethodGet, c.sheetURL("rows", query), nil, &page); err != nil {
			return nil, err
		}
		rows = append(rows, page.Rows...)
		if page.NextCursor == nil {
			return rows, nil
		}
		cursor = *page.NextCursor
	}
}

// Create adds a row and returns it as stored.
func (c *Client) Create(ctx context.Context, row NewRow) (Row, error) {
	var body struct {
		Row Row `json:"row"`
	}
	err := c.do(ctx, http.MethodPost, c.sheetURL("rows", nil), row, &body)
	return body.Row, err
}

func (c *Client) sheetURL(path string, query url.Values) string {
	u := fmt.Sprintf("%s/api/v1/sheets/%s/%s", c.baseURL, url.PathEscape(c.sheet), path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

func (c *Client) do(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gametrak")
	req.Header.Set("CF-Access-Client-Id", c.creds.ClientID)
	req.Header.Set("CF-Access-Client-Secret", c.creds.ClientSecret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Logbook at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError(resp, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &APIError{Status: resp.StatusCode, Message: "response is not Logbook JSON: " + snippet(data)}
	}
	return nil
}

// responseError describes a failed response: Logbook's own error shape when
// present, otherwise what Cloudflare Access most likely meant.
func responseError(resp *http.Response, data []byte) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && body.Error.Code != "" {
		return &APIError{Status: resp.StatusCode, Code: body.Error.Code, Message: body.Error.Message}
	}

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return &APIError{Status: resp.StatusCode, Message: "Cloudflare Access sent the request to its login page; " +
			"check the service token, and that it is in the application's Service Auth policy"}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &APIError{Status: resp.StatusCode, Message: "Cloudflare Access rejected the service token; " +
			"check the client ID and secret, and that the token hasn't expired"}
	}
	return &APIError{Status: resp.StatusCode, Message: snippet(data)}
}

func snippet(data []byte) string {
	text := strings.Join(strings.Fields(string(data)), " ")
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	if text == "" {
		return "(empty response)"
	}
	return text
}
