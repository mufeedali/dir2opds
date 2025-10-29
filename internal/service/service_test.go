package service_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/dubyte/dir2opds/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler(t *testing.T) {
	// pre-setup
	nowFn := service.TimeNow
	defer func() {
		service.TimeNow = nowFn
	}()

	tests := map[string]struct {
		input             string
		want              string
		WantedContentType string
		wantedStatusCode  int
	}{
		"feed (dir of dirs )":                 {input: "/", want: feed, WantedContentType: "application/atom+xml;profile=opds-catalog;kind=navigation", wantedStatusCode: 200},
		"acquisitionFeed(dir of files)":       {input: "/mybook", want: acquisitionFeed, WantedContentType: "application/atom+xml;profile=opds-catalog;kind=acquisition", wantedStatusCode: 200},
		"servingAFile":                        {input: "/mybook/mybook.txt", want: "Fixture", WantedContentType: "text/plain; charset=utf-8", wantedStatusCode: 200},
		"serving file with spaces":            {input: "/mybook/mybook%20copy.txt", want: "Fixture", WantedContentType: "text/plain; charset=utf-8", wantedStatusCode: 200},
		"http trasversal vulnerability check": {input: "/../../../../mybook", want: feed, WantedContentType: "application/atom+xml;profile=opds-catalog;kind=navigation", wantedStatusCode: 404},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// setup
			s := service.OPDS{
				TrustedRoot:      "testdata",
				HideCalibreFiles: true,
				HideDotFiles:     true,
				NoCache:          true,
				GroupFormats:     false,
				AuthorFromFolder: false,
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.input, nil)
			service.TimeNow = func() time.Time {
				return time.Date(2020, 05, 25, 00, 00, 00, 0, time.UTC)
			}

			// act
			err := s.Handler(w, req)
			require.NoError(t, err)

			// post act
			resp := w.Result()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			// verify
			require.Equal(t, tc.wantedStatusCode, resp.StatusCode)
			if tc.wantedStatusCode != http.StatusOK {
				return
			}
			assert.Equal(t, tc.WantedContentType, resp.Header.Get("Content-Type"))
			// normalize whitespace before comparison to avoid indentation differences
			norm := func(s string) string {
				re := regexp.MustCompile(`\s+`)
				return re.ReplaceAllString(s, " ")
			}
			assert.Equal(t, norm(tc.want), norm(string(body)))
		})
	}

}

var feed = `<?xml version="1.0" encoding="UTF-8"?>
  <feed xmlns="http://www.w3.org/2005/Atom">
	  <title>Catalog in /</title>
	  <id>/</id>
	  <link rel="start" href="/" type="application/atom+xml;profile=opds-catalog;kind=navigation"></link>
	  <updated>2020-05-25T00:00:00+00:00</updated>
	  <entry>
		  <title>Knut Hamsun</title>
		  <id>/Knut Hamsun</id>
		  <link rel="subsection" href="/Knut%20Hamsun" type="application/atom+xml;profile=opds-catalog;kind=navigation" title="Knut Hamsun"></link>
		  <published></published>
		  <updated></updated>
	  </entry>
	  <entry>
		  <title>emptyFolder</title>
		  <id>/emptyFolder</id>
		  <link rel="subsection" href="/emptyFolder" type="application/atom+xml;profile=opds-catalog;kind=acquisition" title="emptyFolder"></link>
		  <published></published>
		  <updated></updated>
	  </entry>
	  <entry>
		  <title>multiformat</title>
		  <id>/multiformat</id>
		  <link rel="subsection" href="/multiformat" type="application/atom+xml;profile=opds-catalog;kind=acquisition" title="multiformat"></link>
		  <published></published>
		  <updated></updated>
	  </entry>
	  <entry>
		  <title>mybook</title>
		  <id>/mybook</id>
		  <link rel="subsection" href="/mybook" type="application/atom+xml;profile=opds-catalog;kind=acquisition" title="mybook"></link>
		  <published></published>
		  <updated></updated>
	  </entry>
	  <entry>
		  <title>new folder</title>
		  <id>/new folder</id>
		  <link rel="subsection" href="/new%20folder" type="application/atom+xml;profile=opds-catalog;kind=acquisition" title="new folder"></link>
		  <published></published>
		  <updated></updated>
	  </entry>
  </feed>`

var acquisitionFeed = `<?xml version="1.0" encoding="UTF-8"?>
  <feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/terms/" xmlns:opds="http://opds-spec.org/2010/catalog">
      <title>Catalog in /mybook</title>
      <id>/mybook</id>
      <link rel="start" href="/" type="application/atom+xml;profile=opds-catalog;kind=navigation"></link>
      <updated>2020-05-25T00:00:00+00:00</updated>
      <entry>
          <title>mybook copy.epub</title>
          <id>/mybookmybook copy.epub</id>
          <link rel="http://opds-spec.org/acquisition" href="/mybook/mybook%20copy.epub" type="application/epub+zip" title="mybook copy.epub"></link>
          <published></published>
          <updated></updated>
      </entry>
      <entry>
          <title>mybook copy.txt</title>
          <id>/mybookmybook copy.txt</id>
          <link rel="http://opds-spec.org/acquisition" href="/mybook/mybook%20copy.txt" type="text/plain; charset=utf-8" title="mybook copy.txt"></link>
          <published></published>
          <updated></updated>
      </entry>
      <entry>
          <title>mybook.epub</title>
          <id>/mybookmybook.epub</id>
          <link rel="http://opds-spec.org/acquisition" href="/mybook/mybook.epub" type="application/epub+zip" title="mybook.epub"></link>
          <published></published>
          <updated></updated>
      </entry>
      <entry>
          <title>mybook.pdf</title>
          <id>/mybookmybook.pdf</id>
          <link rel="http://opds-spec.org/acquisition" href="/mybook/mybook.pdf" type="application/pdf" title="mybook.pdf"></link>
          <published></published>
          <updated></updated>
      </entry>
      <entry>
          <title>mybook.txt</title>
          <id>/mybookmybook.txt</id>
          <link rel="http://opds-spec.org/acquisition" href="/mybook/mybook.txt" type="text/plain; charset=utf-8" title="mybook.txt"></link>
          <published></published>
          <updated></updated>
      </entry>
  </feed>`

func TestHandlerWithGroupFormats(t *testing.T) {
	// pre-setup
	nowFn := service.TimeNow
	defer func() {
		service.TimeNow = nowFn
	}()

	tests := map[string]struct {
		input             string
		want              string
		WantedContentType string
		wantedStatusCode  int
		groupFormats      bool
		authorFromFolder  bool
	}{
		"grouped formats": {
			input:             "/multiformat",
			want:              groupedAcquisitionFeed,
			WantedContentType: "application/atom+xml;profile=opds-catalog;kind=acquisition",
			wantedStatusCode:  200,
			groupFormats:      true,
			authorFromFolder:  false,
		},
		"grouped formats with author": {
			input:             "/Knut%20Hamsun/Hunger",
			want:              groupedWithAuthorFeed,
			WantedContentType: "application/atom+xml;profile=opds-catalog;kind=acquisition",
			wantedStatusCode:  200,
			groupFormats:      true,
			authorFromFolder:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// setup
			s := service.OPDS{
				TrustedRoot:      "testdata",
				HideCalibreFiles: true,
				HideDotFiles:     true,
				NoCache:          true,
				GroupFormats:     tc.groupFormats,
				AuthorFromFolder: tc.authorFromFolder,
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.input, nil)
			service.TimeNow = func() time.Time {
				return time.Date(2020, 05, 25, 00, 00, 00, 0, time.UTC)
			}

			// act
			err := s.Handler(w, req)
			require.NoError(t, err)

			// post act
			resp := w.Result()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			// verify
			require.Equal(t, tc.wantedStatusCode, resp.StatusCode)
			if tc.wantedStatusCode != http.StatusOK {
				return
			}
			assert.Equal(t, tc.WantedContentType, resp.Header.Get("Content-Type"))
			re := regexp.MustCompile(`\s+`)
			norm := func(s string) string { return re.ReplaceAllString(s, " ") }
			assert.Equal(t, norm(tc.want), norm(string(body)))
		})
	}
}

var groupedAcquisitionFeed = `<?xml version="1.0" encoding="UTF-8"?>
  <feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/terms/" xmlns:opds="http://opds-spec.org/2010/catalog">
      <title>Catalog in /multiformat</title>
      <id>/multiformat</id>
      <link rel="start" href="/" type="application/atom+xml;profile=opds-catalog;kind=navigation"></link>
      <updated>2020-05-25T00:00:00+00:00</updated>
      <entry>
          <title>Hunger</title>
          <id>/multiformatHunger</id>
          <link rel="http://opds-spec.org/acquisition" href="/multiformat/Hunger.azw3" type="application/vnd.amazon.mobi8-ebook" title="Hunger.azw3"></link>
          <link rel="http://opds-spec.org/acquisition" href="/multiformat/Hunger.epub" type="application/epub+zip" title="Hunger.epub"></link>
          <link rel="http://opds-spec.org/acquisition" href="/multiformat/Hunger.kepub.epub" type="application/epub+zip" title="Hunger.kepub.epub"></link>
          <published></published>
          <updated></updated>
      </entry>
  </feed>`

var groupedWithAuthorFeed = `<?xml version="1.0" encoding="UTF-8"?>
  <feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/terms/" xmlns:opds="http://opds-spec.org/2010/catalog">
      <title>Catalog in /Knut Hamsun/Hunger</title>
      <id>/Knut Hamsun/Hunger</id>
      <link rel="start" href="/" type="application/atom+xml;profile=opds-catalog;kind=navigation"></link>
      <updated>2020-05-25T00:00:00+00:00</updated>
      <entry>
          <title>Hunger</title>
          <id>/Knut Hamsun/HungerHunger</id>
          <link rel="http://opds-spec.org/acquisition" href="/Knut%20Hamsun/Hunger/Hunger.azw3" type="application/vnd.amazon.mobi8-ebook" title="Hunger.azw3"></link>
          <link rel="http://opds-spec.org/acquisition" href="/Knut%20Hamsun/Hunger/Hunger.epub" type="application/epub+zip" title="Hunger.epub"></link>
          <link rel="http://opds-spec.org/acquisition" href="/Knut%20Hamsun/Hunger/Hunger.kepub.epub" type="application/epub+zip" title="Hunger.kepub.epub"></link>
          <published></published>
          <updated></updated>
          <author>
              <name>Knut Hamsun</name>
          </author>
      </entry>
  </feed>`
