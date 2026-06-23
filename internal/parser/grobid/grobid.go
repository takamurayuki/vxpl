package grobid

import (
    "context"
    "encoding/xml"
    "fmt"
    "io"
    "mime/multipart"
    "net/http"
    "time"

    "github.com/takamurayuki/vxpl/internal/parser"
)

// Client is a parser.Engine backed by a running GROBID service.
type Client struct {
    baseURL string
    http    *http.Client
}

// New returns a GROBID-backed engine. baseURL is e.g. "http://localhost:8070".
func New(baseURL string) *Client {
    return &Client{
        baseURL: baseURL,
        http:    &http.Client{Timeout: 120 * time.Second},
    }
}

// Name identifies this engine in logs, metrics and routing.
func (c *Client) Name() string { return "grobid" }

// Capabilities reports what GROBID extracts and how heavy it is.
func (c *Client) Capabilities() parser.Capabilities {
    return parser.Capabilities{
        Supported: parser.FeatureMetadata | parser.FeatureReferences | parser.FeatureBody,
        Profile:   parser.ProfileCPU,
    }
}

// Parse sends the PDF to GROBID and converts the returned TEI into a Document.
func (c *Client) Parse(ctx context.Context, r io.Reader, opts parser.Options) (*parser.Document, error) {
    teiBytes, err := c.processFulltext(ctx, r)
    if err != nil {
        return nil, err
    }

    var doc tei
    if err := xml.Unmarshal(teiBytes, &doc); err != nil {
        return nil, fmt.Errorf("grobid: parse TEI: %w", err)
    }

    out := toDocument(doc, opts.ContentHash)
    out.ParsedAt = time.Now().UTC()
    return out, nil
}

// processFulltext performs the multipart POST to /api/processFulltextDocument.
func (c *Client) processFulltext(ctx context.Context, r io.Reader) ([]byte, error) {
    pr, pw := io.Pipe()
    mw := multipart.NewWriter(pw)

    // Stream the multipart body in a goroutine so we never buffer the whole
    // PDF in memory; the request reads from pr as we write to pw.
    go func() {
        part, err := mw.CreateFormFile("input", "document.pdf")
        if err != nil {
            _ = pw.CloseWithError(err)
            return
        }
        if _, err := io.Copy(part, r); err != nil {
            _ = pw.CloseWithError(err)
            return
        }
        _ = pw.CloseWithError(mw.Close())
    }()

    url := c.baseURL + "/api/processFulltextDocument"
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
    if err != nil {
        return nil, fmt.Errorf("grobid: build request: %w", err)
    }
    req.Header.Set("Content-Type", mw.FormDataContentType())
    req.Header.Set("Accept", "application/xml")

    resp, err := c.http.Do(req)
    if err != nil {
        return nil, fmt.Errorf("grobid: request failed: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("grobid: read response: %w", err)
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("grobid: unexpected status %d: %s", resp.StatusCode, truncate(body, 200))
    }
    return body, nil
}

func truncate(b []byte, n int) string {
    if len(b) > n {
        return string(b[:n])
    }
    return string(b)
}