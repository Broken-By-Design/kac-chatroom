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
		t.Logf("%s rendered %d bytes", name, buf.Len())
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
