package auth

import (
	"context"
	"mime"
	"net/http"
	"strings"
)

// Do sends a request built by newReq, with a bearer token from ts when ts is
// not nil. If the server answers 401 the cached token may have been revoked or
// the issuer re-keyed, so the token is dropped and the request is rebuilt and
// sent once more with a fresh one. The caller closes the response body.
func Do(ctx context.Context, hc *http.Client, ts TokenSource, newReq func() (*http.Request, error)) (*http.Response, error) {
	send := func() (*http.Response, error) {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		if ts != nil {
			token, err := ts.Token(ctx)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return hc.Do(req)
	}

	resp, err := send()
	if err != nil || resp.StatusCode != http.StatusUnauthorized || ts == nil {
		return resp, err
	}
	_ = resp.Body.Close()
	ts.Invalidate()
	return send()
}

// IsJSON reports whether a response declares a JSON body.
func IsJSON(resp *http.Response) bool {
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}
