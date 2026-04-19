// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// Giphy REST client. The broker proxies GIPHY API calls, keeping the GIPHY API
// key server-side. These types mirror the public GIPHY response shape so the
// client can consume them directly — see
// https://developers.giphy.com/docs/api/schema for the canonical schema.

// GiphyImage is a single rendition URL with dimensions and file metadata.
// Different renditions expose different subsets of these fields; unused ones
// are simply empty strings / zero values.
type GiphyImage struct {
	URL      string `json:"url,omitempty"`
	Width    string `json:"width,omitempty"`
	Height   string `json:"height,omitempty"`
	Size     string `json:"size,omitempty"`
	MP4      string `json:"mp4,omitempty"`
	MP4Size  string `json:"mp4_size,omitempty"`
	WebP     string `json:"webp,omitempty"`
	WebPSize string `json:"webp_size,omitempty"`
	Frames   string `json:"frames,omitempty"`
	Hash     string `json:"hash,omitempty"`
}

// GiphyImages collects the full set of renditions returned for a GIF. Only a
// subset is listed here — GIPHY's response contains more entries (preview_gif,
// 480w_still, etc.) which can be added as they become needed.
type GiphyImages struct {
	Original               GiphyImage `json:"original,omitempty"`
	Downsized              GiphyImage `json:"downsized,omitempty"`
	DownsizedLarge         GiphyImage `json:"downsized_large,omitempty"`
	DownsizedMedium        GiphyImage `json:"downsized_medium,omitempty"`
	DownsizedSmall         GiphyImage `json:"downsized_small,omitempty"`
	DownsizedStill         GiphyImage `json:"downsized_still,omitempty"`
	FixedHeight            GiphyImage `json:"fixed_height,omitempty"`
	FixedHeightDownsampled GiphyImage `json:"fixed_height_downsampled,omitempty"`
	FixedHeightSmall       GiphyImage `json:"fixed_height_small,omitempty"`
	FixedHeightStill       GiphyImage `json:"fixed_height_still,omitempty"`
	FixedWidth             GiphyImage `json:"fixed_width,omitempty"`
	FixedWidthDownsampled  GiphyImage `json:"fixed_width_downsampled,omitempty"`
	FixedWidthSmall        GiphyImage `json:"fixed_width_small,omitempty"`
	FixedWidthStill        GiphyImage `json:"fixed_width_still,omitempty"`
	Looping                GiphyImage `json:"looping,omitempty"`
	Preview                GiphyImage `json:"preview,omitempty"`
	PreviewGIF             GiphyImage `json:"preview_gif,omitempty"`
	PreviewWebP            GiphyImage `json:"preview_webp,omitempty"`
	OriginalStill          GiphyImage `json:"original_still,omitempty"`
	OriginalMP4            GiphyImage `json:"original_mp4,omitempty"`
	FourEighty_WStill      GiphyImage `json:"480w_still,omitempty"`
}

// GiphyGIF represents a single GIF as returned from GIPHY endpoints. The `user`
// field is intentionally omitted — we don't surface user data to callers.
type GiphyGIF struct {
	Type             string      `json:"type"`                      // always "gif" (default)
	ID               string      `json:"id"`                        // unique id, e.g. "YsTs5ltWtEhnq"
	Slug             string      `json:"slug"`                      // url-safe slug
	URL              string      `json:"url"`                       // canonical giphy.com URL
	BitlyURL         string      `json:"bitly_url"`                 // shortened gph.is URL
	EmbedURL         string      `json:"embed_url"`                 // giphy.com/embed URL
	Username         string      `json:"username"`                  // uploader username, if any
	Source           string      `json:"source"`                    // URL the GIF was found on
	Rating           string      `json:"rating"`                    // y, g, pg, pg-13, r
	ContentURL       string      `json:"content_url"`               // currently unused per GIPHY docs
	SourceTLD        string      `json:"source_tld"`                // top-level domain of the source
	SourcePostURL    string      `json:"source_post_url"`           // URL of the web page the GIF was found on
	UpdateDatetime   string      `json:"update_datetime"`           // last-updated timestamp (string)
	CreateDatetime   string      `json:"create_datetime"`           // added-to-DB timestamp (string)
	ImportDatetime   string      `json:"import_datetime"`           // original upload/creation timestamp (string)
	TrendingDatetime string      `json:"trending_datetime"`         // trending flag timestamp (string)
	Images           GiphyImages `json:"images"`                    // rendition URLs and metadata
	Title            string      `json:"title"`                     // caption on giphy.com
	AltText          string      `json:"alt_text"`                  // accessibility description
	IsLowContrast    bool        `json:"is_low_contrast,omitempty"` // stickers only
}

// GiphyMeta is the metadata envelope every GIPHY response carries.
// See https://developers.giphy.com/docs/api#response-codes for status values.
type GiphyMeta struct {
	Msg        string `json:"msg"`                   // HTTP response message — "OK" on success (required)
	Status     int    `json:"status"`                // HTTP response code (required)
	ResponseID string `json:"response_id,omitempty"` // unique ID paired with this response
}

// GiphyTranslateResponse is the broker's proxied response for the /translate
// endpoint: a single best-match GIF plus the standard meta envelope.
type GiphyTranslateResponse struct {
	Data GiphyGIF  `json:"data"`
	Meta GiphyMeta `json:"meta"`
}

// GiphyTranslate calls the broker's Giphy /translate proxy. The server holds
// the GIPHY API key and forwards the request upstream; this method only needs
// an authenticated session (same X-Username / X-Signature / Bearer headers as
// every other signedPost).
func GiphyTranslate(req apitypes.GiphyTranslateRequest) (*GiphyTranslateResponse, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("giphy translate: marshal: %w", err)
	}
	resp, err := signedPost(s.v0("/gifs/translate"), s.Username, "/api/v0/gifs/translate", body)
	if err != nil {
		return nil, fmt.Errorf("giphy translate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("giphy translate: broker returned %s", resp.Status)
	}
	var out GiphyTranslateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("giphy translate: decode: %w", err)
	}
	return &out, nil
}
