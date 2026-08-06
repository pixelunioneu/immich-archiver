// Package immich is a minimal client for the Immich REST API, covering only
// what immich-archiver needs: paginated asset search, original-file download,
// and shared-album/shared-library listing.
package immich

import "encoding/json"

// Asset mirrors the subset of Immich's asset JSON we rely on for sync
// decisions. RawJSON preserves the full server response verbatim so it can be
// written out as the sidecar without loss.
type Asset struct {
	ID               string `json:"id"`
	OriginalFileName string `json:"originalFileName"`
	OriginalPath     string `json:"originalPath"`
	Type             string `json:"type"` // "IMAGE" or "VIDEO"
	IsArchived       bool   `json:"isArchived"`
	IsTrashed        bool   `json:"isTrashed"`
	IsFavorite       bool   `json:"isFavorite"`
	FileCreatedAt    string `json:"fileCreatedAt"`
	FileModifiedAt   string `json:"fileModifiedAt"`
	LivePhotoVideoID string `json:"livePhotoVideoId"`
	OwnerID          string `json:"ownerId"`

	// RawJSON is the untouched raw asset object as returned by the server,
	// used verbatim as sidecar content.
	RawJSON json.RawMessage `json:"-"`
}

// UnmarshalJSON captures the raw bytes alongside the parsed fields.
func (a *Asset) UnmarshalJSON(data []byte) error {
	type alias Asset
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*a = Asset(v)
	a.RawJSON = append(json.RawMessage(nil), data...)
	return nil
}

// SearchMetadataQuery is the request body for POST /search/metadata.
//
// WithExif and WithPeople matter more than they look: exifInfo and people are
// optional fields on Immich's asset response, omitted (or left as an empty
// array) unless explicitly requested. Since the sidecar is the server response
// verbatim, leaving them unset silently strips capture date, camera make and
// model, GPS coordinates, rating and faces from every sidecar we write.
type SearchMetadataQuery struct {
	Page        int    `json:"page"`
	Size        int    `json:"size,omitempty"`
	WithExif    bool   `json:"withExif,omitempty"`
	WithPeople  bool   `json:"withPeople,omitempty"`
	WithDeleted bool   `json:"withDeleted,omitempty"`
	IsArchived  *bool  `json:"isArchived,omitempty"`
	PersonalOwn bool   `json:"-"` // filtered client-side, Immich search has no such flag
	AlbumIDs    string `json:"albumIds,omitempty"`
}

type searchMetadataResponse struct {
	Assets struct {
		Total    int      `json:"total"`
		Count    int      `json:"count"`
		Items    []*Asset `json:"items"`
		NextPage string   `json:"nextPage"`
	} `json:"assets"`
}

// Album is the subset of Immich's album JSON we need to discover shared
// albums and their assets.
type Album struct {
	ID        string `json:"id"`
	AlbumName string `json:"albumName"`
	Shared    bool   `json:"shared"`
	OwnerID   string `json:"ownerId"`
}

// AlbumDetail is the response of GET /albums/{id}, including its assets.
type AlbumDetail struct {
	Album
	Assets []*Asset `json:"assets"`
}
