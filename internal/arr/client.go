package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"code.neureka.dev/warrden/warrden/internal/config"
)

// ClientOptions configures one arr HTTP client.
type ClientOptions struct {
	Instance       config.Instance
	RetryCount     int
	AttemptTimeout time.Duration
	BaseDelay      time.Duration
	HTTPClient     *http.Client
}

// Client is a concrete client for one supported arr instance.
type Client struct {
	instance  config.Instance
	baseURL   *url.URL
	http      *http.Client
	retries   int
	timeout   time.Duration
	baseDelay time.Duration
	ownsHTTP  bool
}

// NewHTTPClient creates the shared production HTTP client used by arr instances.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}}
}

// NewClient creates an arr client from a validated instance.
func NewClient(options ClientOptions) *Client {
	base := *options.Instance.URL
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	httpClient := options.HTTPClient
	ownsHTTP := false
	if httpClient == nil {
		httpClient = NewHTTPClient()
		ownsHTTP = true
	}
	baseDelay := options.BaseDelay
	if baseDelay == 0 {
		baseDelay = time.Second
	}
	timeout := options.AttemptTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	retries := max(options.RetryCount, 0)
	return &Client{instance: options.Instance, baseURL: &base, http: httpClient, retries: retries, timeout: timeout, baseDelay: baseDelay, ownsHTTP: ownsHTTP}
}

// Instance returns the configured display name.
func (c *Client) Instance() string { return c.instance.Name }

// Close releases idle HTTP connections owned by the client.
func (c *Client) Close() {
	if !c.ownsHTTP {
		return
	}
	if transport, ok := c.http.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

// Validate returns the authenticated system-status response code.
func (c *Client) Validate(ctx context.Context) (int, string, error) {
	response, err := c.do(ctx, http.MethodGet, c.api("system/status"), nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, statusCodeName(response.StatusCode), nil
}

// Queue returns all current queue records with ID deduplication across mutable pages.
func (c *Client) Queue(ctx context.Context) ([]QueueItem, error) {
	prefix := "includeUnknownMovieItems=true"
	switch {
	case c.instance.Kind == config.Sonarr:
		prefix = "includeUnknownSeriesItems=true"
	case c.instance.Kind == config.Lidarr:
		prefix = "includeUnknownArtistItems=true&includeArtist=true&includeAlbum=true"
	case c.instance.Kind == config.Whisparr && !c.instance.IsWhisparrEros():
		prefix = "includeUnknownSeriesItems=true&includeSeries=true&includeEpisode=true"
	case c.instance.IsWhisparrEros():
		prefix = "includeUnknownMovieItems=true&includeMovie=true"
	}
	return fetchPages(ctx, c, func(page int) string {
		return fmt.Sprintf("%s/queue?%s&page=%d&pageSize=100", c.apiRoot(), prefix, page)
	}, queueItemDTO.domain, func(item QueueItem) int { return item.ID })
}

// DeleteQueue removes a queue item with the requested blocklist behavior.
func (c *Client) DeleteQueue(ctx context.Context, id int, blocklist bool) error {
	query := fmt.Sprintf("blocklist=%t&skipRedownload=false", blocklist)
	if c.instance.Kind == config.Lidarr {
		query = fmt.Sprintf("blocklist=%t&removeFromClient=true", blocklist)
	}
	if c.instance.IsWhisparrEros() {
		query = fmt.Sprintf("removeFromClient=true&blocklist=%t&skipRedownload=false", blocklist)
	}
	return c.expectSuccess(ctx, http.MethodDelete, fmt.Sprintf("%s/queue/%d?%s", c.apiRoot(), id, query), nil)
}

// WantedEpisodes returns every missing or cutoff episode.
func (c *Client) WantedEpisodes(ctx context.Context, cutoff bool) ([]Episode, error) {
	kind := "missing"
	if cutoff {
		kind = "cutoff"
	}
	return fetchPages(ctx, c, func(page int) string {
		return fmt.Sprintf("%s/wanted/%s?includeSeries=true&monitored=true&page=%d&pageSize=100&sortKey=episodes.lastSearchTime&sortDirection=ascending", c.apiRoot(), kind, page)
	}, episodeDTO.domain, func(item Episode) int { return item.ID })
}

// WantedMovies returns every missing or cutoff movie.
func (c *Client) WantedMovies(ctx context.Context, cutoff bool) ([]Movie, error) {
	kind, sortKey := "missing", "lastSearchTime"
	if cutoff {
		kind = "cutoff"
	}
	if c.instance.IsWhisparrEros() {
		sortKey = "movies.lastSearchTime"
	}
	return fetchPages(ctx, c, func(page int) string {
		return fmt.Sprintf("%s/wanted/%s?monitored=true&page=%d&pageSize=100&sortKey=%s&sortDirection=ascending", c.apiRoot(), kind, page, sortKey)
	}, movieDTO.domain, func(item Movie) int { return item.ID })
}

// WantedAlbums returns every missing or cutoff album.
func (c *Client) WantedAlbums(ctx context.Context, cutoff bool) ([]Album, error) {
	kind := "missing"
	if cutoff {
		kind = "cutoff"
	}
	return fetchPages(ctx, c, func(page int) string {
		return fmt.Sprintf("%s/wanted/%s?includeArtist=true&monitored=true&page=%d&pageSize=100&sortKey=albums.lastSearchTime&sortDirection=ascending", c.apiRoot(), kind, page)
	}, albumDTO.domain, func(item Album) int { return item.ID })
}

// SearchEpisodes triggers an EpisodeSearch command.
func (c *Client) SearchEpisodes(ctx context.Context, ids []int) error {
	return c.command(ctx, map[string]any{"name": "EpisodeSearch", "episodeIds": ids})
}

// SearchSeason triggers one SeasonSearch command.
func (c *Client) SearchSeason(ctx context.Context, seriesID, seasonNumber int) error {
	return c.command(ctx, map[string]any{"name": "SeasonSearch", "seriesId": seriesID, "seasonNumber": seasonNumber})
}

// SearchMovies triggers a MoviesSearch command.
func (c *Client) SearchMovies(ctx context.Context, ids []int) error {
	return c.command(ctx, map[string]any{"name": "MoviesSearch", "movieIds": ids})
}

// SearchAlbums triggers an AlbumSearch command.
func (c *Client) SearchAlbums(ctx context.Context, ids []int) error {
	return c.command(ctx, map[string]any{"name": "AlbumSearch", "albumIds": ids})
}

// SearchArtist triggers one ArtistSearch command.
func (c *Client) SearchArtist(ctx context.Context, id int) error {
	return c.command(ctx, map[string]any{"name": "ArtistSearch", "artistId": id})
}

// Indexers returns all indexers.
func (c *Client) Indexers(ctx context.Context) ([]Indexer, error) {
	var records []indexerDTO
	if err := c.getJSON(ctx, c.api("indexer"), &records); err != nil {
		return nil, err
	}
	indexers := make([]Indexer, len(records))
	for index, record := range records {
		indexers[index] = record.domain()
	}
	return indexers, nil
}

// Tags returns all tags.
func (c *Client) Tags(ctx context.Context) ([]Tag, error) {
	var records []tagDTO
	if err := c.getJSON(ctx, c.api("tag"), &records); err != nil {
		return nil, err
	}
	tags := make([]Tag, len(records))
	for index, record := range records {
		tags[index] = record.domain()
	}
	return tags, nil
}

// CreateTag creates a tag.
func (c *Client) CreateTag(ctx context.Context, label string) (Tag, error) {
	var dto tagDTO
	body, err := json.Marshal(map[string]string{"label": label})
	if err != nil {
		return Tag{}, fmt.Errorf("encode tag: %w", err)
	}
	if err := c.json(ctx, http.MethodPost, c.api("tag"), body, &dto); err != nil {
		return Tag{}, err
	}
	return dto.domain(), nil
}

// EnsureSeriesTag adds a tag to a series if absent.
func (c *Client) EnsureSeriesTag(ctx context.Context, id, tagID int) (bool, error) {
	return c.ensureTag(ctx, "series", id, tagID)
}

// EnsureMovieTag adds a tag to a movie if absent.
func (c *Client) EnsureMovieTag(ctx context.Context, id, tagID int) (bool, error) {
	return c.ensureTag(ctx, "movie", id, tagID)
}

// EnsureArtistTag adds a tag to an artist if absent.
func (c *Client) EnsureArtistTag(ctx context.Context, id, tagID int) (bool, error) {
	return c.ensureTag(ctx, "artist", id, tagID)
}

// ResolveSeriesIDs resolves episode IDs to their parent series IDs in API-sized batches.
func (c *Client) ResolveSeriesIDs(ctx context.Context, ids []int) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	for start := 0; start < len(ids); start += 50 {
		end := min(start+50, len(ids))
		query := repeatedQuery("episodeIds", ids[start:end])
		var records []episodeParentDTO
		if err := c.getJSON(ctx, c.api("episode")+"?"+query+"&includeSeries=true", &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			if record.SeriesID != 0 {
				result[record.SeriesID] = struct{}{}
			}
		}
	}
	return result, nil
}

// ResolveArtistIDs resolves album IDs to parent artist IDs in API-sized batches.
func (c *Client) ResolveArtistIDs(ctx context.Context, ids []int) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	for start := 0; start < len(ids); start += 50 {
		end := min(start+50, len(ids))
		query := repeatedQuery("albumIds", ids[start:end])
		var records []albumParentDTO
		if err := c.getJSON(ctx, c.api("album")+"?"+query, &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			if record.ArtistID != 0 {
				result[record.ArtistID] = struct{}{}
			}
		}
	}
	return result, nil
}

func fetchPages[T, D any](ctx context.Context, client *Client, endpoint func(int) string, convert func(D) T, identity func(T) int) ([]T, error) {
	items := make([]T, 0, 100)
	indexes := make(map[int]int, 100)
	for pageNumber := 1; ; pageNumber++ {
		var response pageDTO[D]
		if err := client.getJSON(ctx, endpoint(pageNumber), &response); err != nil {
			return nil, err
		}
		if len(response.Records) == 0 {
			break
		}
		for _, record := range response.Records {
			item := convert(record)
			id := identity(item)
			if index, exists := indexes[id]; exists {
				items[index] = item
			} else {
				indexes[id] = len(items)
				items = append(items, item)
			}
		}
		pageSize := response.PageSize
		if pageSize <= 0 {
			pageSize = 100
		}
		if len(response.Records) < pageSize || len(items) >= response.TotalRecords {
			break
		}
	}
	return items, nil
}

func (c *Client) command(ctx context.Context, command any) error {
	body, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("encode command: %w", err)
	}
	return c.expectSuccess(ctx, http.MethodPost, c.api("command"), body)
}

func (c *Client) ensureTag(ctx context.Context, resource string, id, tagID int) (bool, error) {
	endpoint := fmt.Sprintf("%s/%s/%d", c.apiRoot(), resource, id)
	var doc document
	if err := c.getJSON(ctx, endpoint, &doc); err != nil {
		return false, err
	}
	if doc == nil {
		return false, nil
	}
	var tags []int
	if raw, exists := doc["tags"]; exists && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &tags); err != nil {
			return false, fmt.Errorf("decode resource tags: %w", err)
		}
	}
	for _, existing := range tags {
		if existing == tagID {
			return false, nil
		}
	}
	tags = append(tags, tagID)
	raw, err := json.Marshal(tags)
	if err != nil {
		return false, fmt.Errorf("encode resource tags: %w", err)
	}
	doc["tags"] = raw
	body, err := json.Marshal(doc)
	if err != nil {
		return false, fmt.Errorf("encode tagged resource: %w", err)
	}
	if err := c.expectSuccess(ctx, http.MethodPut, endpoint, body); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, target any) error {
	return c.json(ctx, http.MethodGet, endpoint, nil, target)
}

func (c *Client) json(ctx context.Context, method, endpoint string, body []byte, target any) error {
	response, err := c.do(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return &HTTPError{StatusCode: response.StatusCode, Status: responseReason(response)}
	}
	if target == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("drain %s response: %w", endpoint, err)
	}
	return nil
}

func (c *Client) expectSuccess(ctx context.Context, method, endpoint string, body []byte) error {
	return c.json(ctx, method, endpoint, body, nil)
}

func (c *Client) do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
		requestURL, err := c.resolve(endpoint)
		if err != nil {
			cancel()
			return nil, err
		}
		request, err := http.NewRequestWithContext(attemptCtx, method, requestURL, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create request: %w", err)
		}
		request.Header.Set("X-Api-Key", c.instance.APIKey)
		if body != nil {
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		response, err := c.http.Do(request)
		if err == nil && !retryStatus(response.StatusCode) {
			response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
			return response, nil
		}
		if err != nil && ctx.Err() != nil {
			cancel()
			return nil, ctx.Err()
		}
		if err != nil && !transientTransportError(err) {
			cancel()
			return nil, &TransportError{Err: err}
		}
		if err == nil {
			if attempt == c.retries {
				response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
				return response, nil
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			lastErr = &HTTPError{StatusCode: response.StatusCode, Status: http.StatusText(response.StatusCode)}
		} else {
			lastErr = &TransportError{Err: err}
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt == c.retries {
			break
		}
		if err := wait(ctx, jitter(c.baseDelay, attempt)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) resolve(endpoint string) (string, error) {
	reference, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	return c.baseURL.ResolveReference(reference).String(), nil
}

func (c *Client) api(path string) string { return c.apiRoot() + "/" + path }

func (c *Client) apiRoot() string {
	if c.instance.Kind == config.Lidarr {
		return "api/v1"
	}
	return "api/v3"
}

func retryStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}

func statusCodeName(code int) string {
	names := map[int]string{
		100: "Continue", 101: "SwitchingProtocols", 102: "Processing", 103: "EarlyHints",
		200: "OK", 201: "Created", 202: "Accepted", 203: "NonAuthoritativeInformation", 204: "NoContent",
		205: "ResetContent", 206: "PartialContent", 207: "MultiStatus", 208: "AlreadyReported", 226: "IMUsed",
		300: "MultipleChoices", 301: "MovedPermanently", 302: "Found", 303: "SeeOther", 304: "NotModified",
		305: "UseProxy", 306: "Unused", 307: "RedirectKeepVerb", 308: "PermanentRedirect",
		400: "BadRequest", 401: "Unauthorized", 402: "PaymentRequired", 403: "Forbidden", 404: "NotFound",
		405: "MethodNotAllowed", 406: "NotAcceptable", 407: "ProxyAuthenticationRequired", 408: "RequestTimeout",
		409: "Conflict", 410: "Gone", 411: "LengthRequired", 412: "PreconditionFailed", 413: "RequestEntityTooLarge",
		414: "RequestUriTooLong", 415: "UnsupportedMediaType", 416: "RequestedRangeNotSatisfiable",
		417: "ExpectationFailed", 421: "MisdirectedRequest", 422: "UnprocessableEntity", 423: "Locked",
		424: "FailedDependency", 426: "UpgradeRequired", 428: "PreconditionRequired", 429: "TooManyRequests",
		431: "RequestHeaderFieldsTooLarge", 451: "UnavailableForLegalReasons",
		500: "InternalServerError", 501: "NotImplemented", 502: "BadGateway", 503: "ServiceUnavailable",
		504: "GatewayTimeout", 505: "HttpVersionNotSupported", 506: "VariantAlsoNegotiates",
		507: "InsufficientStorage", 508: "LoopDetected", 510: "NotExtended", 511: "NetworkAuthenticationRequired",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return strconv.Itoa(code)
}

func responseReason(response *http.Response) string {
	prefix := strconv.Itoa(response.StatusCode) + " "
	if strings.HasPrefix(response.Status, prefix) {
		return strings.TrimPrefix(response.Status, prefix)
	}
	return http.StatusText(response.StatusCode)
}

func transientTransportError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.Timeout() || dnsErr.IsTemporary
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func jitter(base time.Duration, attempt int) time.Duration {
	const maximum = 30 * time.Second
	if base <= 0 {
		return 0
	}
	delay := min(base, maximum)
	for range attempt {
		if delay >= maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	return delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
}

func wait(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func repeatedQuery(name string, ids []int) string {
	parts := make([]string, len(ids))
	for index, id := range ids {
		parts[index] = name + "=" + strconv.Itoa(id)
	}
	return strings.Join(parts, "&")
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
