package main

import (
	"bytes"
	"testing"
)

func TestTemplatesRender(t *testing.T) {
	initTemplates()
	cases := []string{
		"decoy.html",
		"login.html",
		"nickname.html",
		"BANNED.html",
		"chatroom.html",
		"admin/admin-login.html",
		"admin/admin.html",
		"tests/tests.html",
		"video_chat.html",
		"movie.html",
		"movie_list.html",
		"gamble.html",
		"video_search.html",
		"pain.html",
	}
	for _, name := range cases {
		data := map[string]any{
			"nickname": "alice",
			"error":    "",
			"expiry":   "2026-08-07 12:00:00 CDT",
			"movies":   []map[string]string{{"name": "x", "thumb": "t", "file_id": "f"}},
			"fid":      "abc",
			"movie_url": "http://example.com/v.mp4",
		}
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
			t.Fatalf("template %s failed: %v", name, err)
		}
		if buf.Len() == 0 {
			t.Fatalf("template %s rendered empty", name)
		}
	var bufVSearch bytes.Buffer
	if err := templates.ExecuteTemplate(&bufVSearch, "video_search.html", map[string]any{
		"nickname": "alice",
		"expiry":   "2026-08-07 12:00:00 CDT",
	}); err != nil {
		t.Fatalf("video_search.html render failed: %v", err)
	}
	vs := bufVSearch.Bytes()
	// Single stream URLs are bound to the server's cookie/PO-token session and
	// 403 when the browser loads them directly. They must go through the relay.
	if bytes.Contains(vs, []byte(`videoEl.src = data.single`)) {
		t.Fatalf("video_search.html assigns single stream URL directly; " +
			"it must be proxied via /api/video/relay")
	}
	if !bytes.Contains(vs, []byte(`/api/video/relay?url="`)) ||
		!bytes.Contains(vs, []byte(`encodeURIComponent(data.single)`)) {
		t.Fatalf("video_search.html single fallback does not route through the relay")
	}
}
	dataPermanent := map[string]any{"expiry": "Permanent"}
	var bufP bytes.Buffer
	if err := templates.ExecuteTemplate(&bufP, "BANNED.html", dataPermanent); err != nil {
		t.Fatalf("BANNED(permanent) failed: %v", err)
	}
	if !bytes.Contains(bufP.Bytes(), []byte("permanent")) {
		t.Fatalf("BANNED permanent branch missing")
	}

	var bufChat bytes.Buffer
	if err := templates.ExecuteTemplate(&bufChat, "chatroom.html", map[string]any{"nickname": "alice"}); err != nil {
		t.Fatalf("chatroom.html render failed: %v", err)
	}
	for _, want := range []string{`id="imageLightbox"`, `class="image-lightbox-img"`} {
		if !bytes.Contains(bufChat.Bytes(), []byte(want)) {
			t.Fatalf("chatroom.html missing %s", want)
		}
	}
}
