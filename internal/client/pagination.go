// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// DefaultPageLimit is the default page size requested on paginated list endpoints.
	DefaultPageLimit = 100
	// MaxPaginationPages is the upper bound on pages fetched in a single ListAll call
	// to guard against infinite pagination loops from upstream bugs.
	MaxPaginationPages = 1000
)

// Page represents a single page of items from an Anthropic list endpoint.
// It supports both ID-based cursor pagination (first_id / last_id -> after_id)
// and opaque token pagination (next_page -> page).
type Page[T any] struct {
	Data     []T     `json:"data"`
	HasMore  bool    `json:"has_more"`
	FirstID  string  `json:"first_id"`
	LastID   string  `json:"last_id"`
	NextPage *string `json:"next_page"`
}

// ListAll fetches all pages from path using either opaque next_page tokens (?page=...)
// or ID-based cursors (?after_id=...). It detects stalled/repeating cursors and enforces
// MaxPaginationPages so a misbehaving server cannot cause an infinite loop.
func ListAll[T any](ctx context.Context, c *Client, path string, baseQuery url.Values, opts ...RequestOption) ([]T, error) {
	var all []T
	visitedCursors := make(map[string]struct{})

	var nextPageToken string
	var afterIDCursor string

	for pageNum := 0; pageNum < MaxPaginationPages; pageNum++ {
		q := cloneValues(baseQuery)
		if q.Get("limit") == "" {
			q.Set("limit", strconv.Itoa(DefaultPageLimit))
		}
		if nextPageToken != "" {
			q.Set("page", nextPageToken)
		} else if afterIDCursor != "" {
			q.Set("after_id", afterIDCursor)
		}

		reqOpts := make([]RequestOption, 0, len(opts)+1)
		reqOpts = append(reqOpts, opts...)
		reqOpts = append(reqOpts, WithQuery(q))

		var page Page[T]
		if err := c.Do(ctx, http.MethodGet, path, nil, &page, reqOpts...); err != nil {
			return nil, err
		}

		all = append(all, page.Data...)

		// 1. Check opaque cursor pagination (next_page -> ?page=...)
		if page.NextPage != nil && *page.NextPage != "" {
			cursorKey := "page:" + *page.NextPage
			if _, seen := visitedCursors[cursorKey]; seen {
				return nil, fmt.Errorf("pagination cursor loop detected on %s: repeated next_page token %q", path, *page.NextPage)
			}
			visitedCursors[cursorKey] = struct{}{}
			nextPageToken = *page.NextPage
			afterIDCursor = ""
			continue
		}

		// 2. Check ID-based cursor pagination (has_more + last_id -> ?after_id=...)
		if page.HasMore && page.LastID != "" {
			cursorKey := "after_id:" + page.LastID
			if _, seen := visitedCursors[cursorKey]; seen {
				return nil, fmt.Errorf("pagination cursor loop detected on %s: repeated last_id cursor %q", path, page.LastID)
			}
			visitedCursors[cursorKey] = struct{}{}
			afterIDCursor = page.LastID
			nextPageToken = ""
			continue
		}

		return all, nil
	}

	return nil, fmt.Errorf("pagination exceeded maximum page limit (%d) on %s", MaxPaginationPages, path)
}

func cloneValues(src url.Values) url.Values {
	dst := make(url.Values, len(src))
	for k, vv := range src {
		cp := make([]string, len(vv))
		copy(cp, vv)
		dst[k] = cp
	}
	return dst
}
