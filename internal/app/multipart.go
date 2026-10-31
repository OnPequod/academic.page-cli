package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/OnPequod/academic.page-cli/internal/api"
)

// MaxUploadBytes is the largest file the API accepts: the per-file limit of
// the Pro plans, which the API requires. The server enforces the real limit;
// this only saves sending a file that is certain to be refused.
const MaxUploadBytes = 1 << 30

const defaultPartSize = 8 << 20

type multipartInit struct {
	UploadID     string `json:"upload_id"`
	PartSizeHint int64  `json:"part_size_hint"`
}

type multipartSign struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type completedPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

// multipartUpload runs init, sign and PUT for each part, then complete. It
// aborts the upload if any step fails. It returns errMultipartUnsupported
// when the site's storage cannot take multipart uploads.
func (a *App) multipartUpload(ctx context.Context, site int64, info *FileInfo, fields map[string]any) (json.RawMessage, error) {
	base := strings.TrimSuffix(a.Registry.Spec.Operations["manageMultipartInit"].Path, "/init")
	base = strings.ReplaceAll(base, "{site}", fmt.Sprint(site))

	initBody, _ := json.Marshal(map[string]any{"filename": info.Name, "content_type": contentTypeOf(info.Path), "size_bytes": info.Size})
	resp, err := a.Client.Do(ctx, api.Request{Method: "POST", Path: base + "/init", Body: initBody, ContentType: "application/json"})
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.Code == "multipart_upload_not_supported" && !isLimitMessage(apiErr.Message) {
			return nil, errMultipartUnsupported
		}
		return nil, err
	}
	var init multipartInit
	if err := json.Unmarshal(resp.Body, &init); err != nil || init.UploadID == "" {
		return nil, fmt.Errorf("multipart init returned no upload_id: %s", resp.Body)
	}
	session := base + "/" + init.UploadID

	data, err := a.uploadParts(ctx, session, info, init.PartSizeHint, fields)
	if err != nil {
		// Best effort: free the storage the parts used.
		_, _ = a.Client.Do(context.WithoutCancel(ctx), api.Request{Method: "POST", Path: session + "/abort"})
		return nil, err
	}
	return data, nil
}

func (a *App) uploadParts(ctx context.Context, session string, info *FileInfo, partSize int64, fields map[string]any) (json.RawMessage, error) {
	if partSize <= 0 {
		partSize = defaultPartSize
	}
	if parts := (info.Size + partSize - 1) / partSize; parts > 10000 {
		partSize = (info.Size + 9999) / 10000
	}
	f, err := os.Open(info.Path)
	if err != nil {
		return nil, Usagef("cannot read %s: %v", info.Path, err)
	}
	defer f.Close()

	buf := make([]byte, partSize)
	var parts []completedPart
	for number := 1; ; number++ {
		n, err := io.ReadFull(f, buf)
		if n == 0 {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("read %s: %w", info.Path, err)
		}

		signBody, _ := json.Marshal(map[string]int{"part_number": number})
		resp, err := a.Client.Do(ctx, api.Request{Method: "POST", Path: session + "/sign", Body: signBody, ContentType: "application/json"})
		if err != nil {
			return nil, err
		}
		var sign multipartSign
		if err := json.Unmarshal(resp.Body, &sign); err != nil || sign.URL == "" {
			return nil, fmt.Errorf("multipart sign returned no url for part %d", number)
		}
		etag, err := a.Client.PutPart(ctx, sign.Method, sign.URL, buf[:n])
		if err != nil {
			return nil, fmt.Errorf("upload part %d: %w", number, err)
		}
		parts = append(parts, completedPart{PartNumber: number, ETag: etag})
		if n < len(buf) {
			break
		}
	}

	complete := map[string]any{"parts": parts}
	for k, v := range fields {
		complete[k] = v
	}
	body, _ := json.Marshal(complete)
	resp, err := a.Client.Do(ctx, api.Request{Method: "POST", Path: session + "/complete", Body: body, ContentType: "application/json"})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

var errMultipartUnsupported = errors.New("multipart uploads are not supported for this site")

// isLimitMessage reports whether a multipart_upload_not_supported error is
// really a file size or storage quota refusal. The API uses that one code for
// all three, so only the message tells them apart, and falling back to a
// single upload would not help with a limit.
func isLimitMessage(msg string) bool {
	msg = strings.ToLower(msg)
	for _, s := range []string{"larger than", "quota", "limit", "storage is full", "too large"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
