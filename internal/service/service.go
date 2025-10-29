// package service provides a http handler that reads the path in the request.url and returns
// an xml document that follows the OPDS 1.1 standard
// https://specs.opds.io/opds-1.1.html
package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dubyte/dir2opds/opds"
	"golang.org/x/tools/blog/atom"
)

func init() {
	_ = mime.AddExtensionType(".mobi", "application/x-mobipocket-ebook")
	_ = mime.AddExtensionType(".epub", "application/epub+zip")
	_ = mime.AddExtensionType(".cbz", "application/x-cbz")
	_ = mime.AddExtensionType(".cbr", "application/x-cbr")
	_ = mime.AddExtensionType(".fb2", "text/fb2+xml")
	_ = mime.AddExtensionType(".pdf", "application/pdf")
}

const (
	pathTypeFile = iota
	pathTypeDirOfDirs
	pathTypeDirOfFiles
)

const (
	ignoreFile       = true
	includeFile      = false
	currentDirectory = "."
	parentDirectory  = ".."
	hiddenFilePrefix = "."
)

type OPDS struct {
	TrustedRoot      string
	HideCalibreFiles bool
	HideDotFiles     bool
	NoCache          bool
	GroupFormats     bool
	AuthorFromFolder bool
}

type IsDirer interface {
	IsDir() bool
}

func isFile(e IsDirer) bool {
	return !e.IsDir()
}

const navigationType = "application/atom+xml;profile=opds-catalog;kind=navigation"

var TimeNow = timeNowFunc()

// Handler serve the content of a book file or
// returns an Acquisition Feed when the entries are documents or
// returns an Navegation Feed when the entries are other folders
func (s OPDS) Handler(w http.ResponseWriter, req *http.Request) error {
	var err error
	urlPath, err := url.PathUnescape(req.URL.Path)
	if err != nil {
		log.Printf("error while serving '%s': %s", req.URL.Path, err)
		return err
	}

	fPath := filepath.Join(s.TrustedRoot, urlPath)

	// verifyPath avoid the http transversal by checking the path is under DirRoot
	_, err = verifyPath(fPath, s.TrustedRoot)
	if err != nil {
		log.Printf("fPath %q err: %s", fPath, err)
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	log.Printf("urlPath:'%s'", urlPath)

	if _, err := os.Stat(fPath); err != nil {
		log.Printf("fPath err: %s", err)
		w.WriteHeader(http.StatusNotFound)
		return err
	}

	log.Printf("fPath:'%s'", fPath)

	// it's a file just serve the file
	if getPathType(fPath) == pathTypeFile {
		http.ServeFile(w, req, fPath)
		return nil
	}

	if s.NoCache {
		w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Add("Expires", "0")
	}

	navFeed := s.makeFeed(fPath, req)

	var content []byte
	// it is an acquisition feed
	if getPathType(fPath) == pathTypeDirOfFiles {
		acFeed := &opds.AcquisitionFeed{Feed: &navFeed, Dc: "http://purl.org/dc/terms/", Opds: "http://opds-spec.org/2010/catalog"}
		content, err = xml.MarshalIndent(acFeed, "  ", "    ")
		w.Header().Add("Content-Type", "application/atom+xml;profile=opds-catalog;kind=acquisition")
	} else { // it is a navegation feed
		content, err = xml.MarshalIndent(navFeed, "  ", "    ")
		w.Header().Add("Content-Type", "application/atom+xml;profile=opds-catalog;kind=navigation")
	}
	if err != nil {
		log.Printf("error while serving '%s': %s", fPath, err)
		return err
	}

	content = append([]byte(xml.Header), content...)
	http.ServeContent(w, req, "feed.xml", TimeNow(), bytes.NewReader(content))

	return nil
}

// bookFormat represents a single format of a book
type bookFormat struct {
	filename  string
	extension string
}

// bookFormatRecursive includes the relative path from the current feed directory
// so links can be built for nested files when we recurse.
type bookFormatRecursive struct {
	filename  string // base filename (without any path)
	relpath   string // relative path from the feed directory to the file's dir
	extension string
}

// extractBaseName removes known ebook extensions from filename
func extractBaseName(filename string) string {
	name := filename

	// Handle compound extensions like .kepub.epub, .advanced.epub
	for {
		ext := filepath.Ext(name)
		if ext == "" {
			break
		}
		name = strings.TrimSuffix(name, ext)
	}

	return name
}

// groupFilesByBaseName groups files that share the same base name
// groupFilesByBaseName groups files by their base name for the immediate directory.
func groupFilesByBaseName(entries []os.DirEntry, hideCalibreFiles, hideDotFiles bool) map[string][]bookFormat {
	groups := make(map[string][]bookFormat)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()

		if fileShouldBeIgnored(filename, hideCalibreFiles, hideDotFiles) {
			continue
		}

		ext := filepath.Ext(filename)

		baseName := extractBaseName(filename)
		groups[baseName] = append(groups[baseName], bookFormat{
			filename:  filename,
			extension: ext,
		})
	}

	return groups
}

// groupFilesByBaseNameRecursive walks the directory tree rooted at dirPath and
// groups files found under it by their base name, keeping the relative path
// for each file so links point to the correct nested locations.
func groupFilesByBaseNameRecursive(dirPath string, hideCalibreFiles, hideDotFiles bool) map[string][]bookFormatRecursive {
	groups := make(map[string][]bookFormatRecursive)

	filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// ignore errors visiting certain files/dirs
			return nil
		}

		if d.IsDir() {
			return nil
		}

		filename := d.Name()
		if fileShouldBeIgnored(filename, hideCalibreFiles, hideDotFiles) {
			return nil
		}

		ext := filepath.Ext(filename)
		base := extractBaseName(filename)

		// compute relative directory path from dirPath to file's directory
		relDir := filepath.Dir(strings.TrimPrefix(path, dirPath))
		relDir = strings.TrimPrefix(relDir, string(filepath.Separator))

		groups[base] = append(groups[base], bookFormatRecursive{
			filename:  filename,
			relpath:   relDir,
			extension: ext,
		})

		return nil
	})

	return groups
}

// extractAuthorFromPath extracts the author name from the parent folder
func extractAuthorFromPath(fpath string, trustedRoot string) string {
	parentPath := filepath.Dir(fpath)

	// Don't use the root directory as author
	if parentPath == trustedRoot || parentPath == "." || parentPath == "/" {
		return ""
	}

	author := filepath.Base(parentPath)

	// Don't use generic folder names as authors
	if author == "." || author == "/" {
		return ""
	}

	return author
}

func (s OPDS) makeFeed(fpath string, req *http.Request) atom.Feed {
	feedBuilder := opds.FeedBuilder.
		ID(req.URL.Path).
		Title("Catalog in " + req.URL.Path).
		Updated(TimeNow()).
		AddLink(opds.LinkBuilder.Rel("start").Href("/").Type(navigationType).Build())

	dirEntries, _ := os.ReadDir(fpath)

	var entries []atom.Entry
	if s.GroupFormats {
		entries = buildGroupedFeed(fpath, dirEntries, req, s)
	} else {
		entries = buildStandardFeed(fpath, dirEntries, req, s)
	}

	for _, e := range entries {
		feedBuilder = feedBuilder.AddEntry(e)
	}

	return feedBuilder.Build()
}

// mkLink constructs an opds.Link for a given filename and path type
func mkLink(name string, pathType int, req *http.Request) atom.Link {
	return opds.LinkBuilder.
		Rel(getRel(name, pathType)).
		Title(name).
		Href(filepath.Join(req.URL.RequestURI(), url.PathEscape(name))).
		Type(getType(name, pathType)).
		Build()
}

// mkLinkForRelPath builds a link for a file that may be in a nested relative
// path from the current feed directory. It properly escapes each path segment
// and appends it to the request URL.
func mkLinkForRelPath(dirRelPath, filename string, pathType int, req *http.Request) atom.Link {
	// build href parts: requestURI + dirRelPath (if any) + filename
	// ensure each segment is escaped
	parts := []string{req.URL.RequestURI()}
	if dirRelPath != "" {
		for _, seg := range strings.Split(dirRelPath, string(filepath.Separator)) {
			if seg == "" {
				continue
			}
			parts = append(parts, url.PathEscape(seg))
		}
	}
	parts = append(parts, url.PathEscape(filename))

	href := strings.Join(parts, "/")

	return opds.LinkBuilder.
		Rel(getRel(filename, pathType)).
		Title(filename).
		Href(href).
		Type(getType(filename, pathType)).
		Build()
}

// buildEntry builds an atom.Entry from id, title, links and optional author
func buildEntry(id, title string, links []atom.Link, author string) atom.Entry {
	eb := opds.EntryBuilder.ID(id).Title(title)
	if author != "" {
		eb = eb.Author(&atom.Person{Name: author})
	}
	for _, l := range links {
		eb = eb.AddLink(l)
	}
	return eb.Build()
}

// buildGroupedFeed encapsulates the grouped branch of makeFeed
func buildGroupedFeed(fpath string, dirEntries []os.DirEntry, req *http.Request, s OPDS) []atom.Entry {
	// If the current directory contains subdirectories, perform a recursive
	// grouping across the subtree so the feed can list every book under this
	// path in a single listing. Otherwise, group only files in the current dir.
	hasSubdirs := false
	for _, de := range dirEntries {
		if de.IsDir() {
			hasSubdirs = true
			break
		}
	}

	var entries []atom.Entry
	var author string
	if s.AuthorFromFolder {
		author = extractAuthorFromPath(fpath, s.TrustedRoot)
	}

	if hasSubdirs {
		// recursive grouping
		recGroups := groupFilesByBaseNameRecursive(fpath, s.HideCalibreFiles, s.HideDotFiles)

		// include directory entries (immediate subsections)
		for _, entry := range dirEntries {
			if entry.IsDir() {
				name := entry.Name()
				if fileShouldBeIgnored(name, s.HideCalibreFiles, s.HideDotFiles) {
					continue
				}

				pathType := getPathType(filepath.Join(fpath, name))
				l := mkLink(name, pathType, req)
				e := buildEntry(req.URL.Path+name, name, []atom.Link{l}, "")
				entries = append(entries, e)
			}
		}

		// build entries from recursive groups; note groups are by base name
		// across the subtree so multiple formats from different subdirs will
		// be combined if they share the same base name.
		for baseName, formats := range recGroups {
			var links []atom.Link
			// choose first file's path type to detect nested directories vs files
			for _, f := range formats {
				// compute path type by checking actual file
				p := filepath.Join(fpath, f.relpath, f.filename)
				pathType := getPathType(p)
				links = append(links, mkLinkForRelPath(f.relpath, f.filename, pathType, req))
			}

			e := buildEntry(req.URL.Path+baseName, baseName, links, author)
			entries = append(entries, e)
		}

		return entries
	}
	// no subdirs: group only immediate files (existing behavior)
	groups := groupFilesByBaseName(dirEntries, s.HideCalibreFiles, s.HideDotFiles)

	// include any directory entries (should be none here) for completeness
	for _, entry := range dirEntries {
		if entry.IsDir() {
			name := entry.Name()
			if fileShouldBeIgnored(name, s.HideCalibreFiles, s.HideDotFiles) {
				continue
			}

			pathType := getPathType(filepath.Join(fpath, name))
			l := mkLink(name, pathType, req)
			e := buildEntry(req.URL.Path+name, name, []atom.Link{l}, "")
			entries = append(entries, e)
		}
	}

	for baseName, formats := range groups {
		// Determine type using first format
		firstPath := filepath.Join(fpath, formats[0].filename)
		pathType := getPathType(firstPath)

		if pathType == pathTypeDirOfDirs || pathType == pathTypeDirOfFiles {
			l := mkLink(formats[0].filename, pathType, req)
			e := buildEntry(req.URL.Path+formats[0].filename, formats[0].filename, []atom.Link{l}, "")
			entries = append(entries, e)
			continue
		}

		// For files, build entry with multiple acquisition links
		var links []atom.Link
		for _, format := range formats {
			links = append(links, mkLink(format.filename, pathTypeFile, req))
		}

		e := buildEntry(req.URL.Path+baseName, baseName, links, author)
		entries = append(entries, e)
	}

	return entries
}

// buildStandardFeed encapsulates the original non-grouped branch of makeFeed
func buildStandardFeed(fpath string, dirEntries []os.DirEntry, req *http.Request, s OPDS) []atom.Entry {
	var entries []atom.Entry
	for _, entry := range dirEntries {
		if fileShouldBeIgnored(entry.Name(), s.HideCalibreFiles, s.HideDotFiles) {
			continue
		}

		pathType := getPathType(filepath.Join(fpath, entry.Name()))
		l := mkLink(entry.Name(), pathType, req)

		author := ""
		if s.AuthorFromFolder && pathType == pathTypeFile {
			author = extractAuthorFromPath(fpath, s.TrustedRoot)
		}

		e := buildEntry(req.URL.Path+entry.Name(), entry.Name(), []atom.Link{l}, author)
		entries = append(entries, e)
	}

	return entries
}

func fileShouldBeIgnored(filename string, hideCalibreFiles, hideDotFiles bool) bool {
	// not ignore those directories
	if filename == currentDirectory || filename == parentDirectory {
		return includeFile
	}

	if hideDotFiles && strings.HasPrefix(filename, hiddenFilePrefix) {
		return ignoreFile
	}

	if hideCalibreFiles &&
		(strings.Contains(filename, ".opf") ||
			strings.Contains(filename, "cover.") ||
			strings.Contains(filename, "metadata.db") ||
			strings.Contains(filename, "metadata_db_prefs_backup.json") ||
			strings.Contains(filename, ".caltrash") ||
			strings.Contains(filename, ".calnotes")) {
		return ignoreFile
	}

	return false
}

func getRel(name string, pathType int) string {
	if pathType == pathTypeDirOfFiles || pathType == pathTypeDirOfDirs {
		return "subsection"
	}

	ext := filepath.Ext(name)
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" {
		return "http://opds-spec.org/image/thumbnail"
	}

	// mobi, epub, etc
	return "http://opds-spec.org/acquisition"
}

func getType(name string, pathType int) string {
	switch pathType {
	case pathTypeFile:
		return mime.TypeByExtension(filepath.Ext(name))
	case pathTypeDirOfFiles:
		return "application/atom+xml;profile=opds-catalog;kind=acquisition"
	case pathTypeDirOfDirs:
		return "application/atom+xml;profile=opds-catalog;kind=navigation"
	default:
		return mime.TypeByExtension("xml")
	}
}

func getPathType(dirpath string) int {
	fi, err := os.Stat(dirpath)
	if err != nil {
		log.Printf("getPathType os.Stat err: %s", err)
	}

	if isFile(fi) {
		return pathTypeFile
	}

	dirEntries, err := os.ReadDir(dirpath)
	if err != nil {
		log.Printf("getPathType: readDir err: %s", err)
	}

	for _, entry := range dirEntries {
		if isFile(entry) {
			return pathTypeDirOfFiles
		}
	}
	// Directory of directories
	return pathTypeDirOfDirs
}

func timeNowFunc() func() time.Time {
	t := time.Now()
	return func() time.Time { return t }
}

// verify path use a trustedRoot to avoid http transversal
// from https://www.stackhawk.com/blog/golang-path-traversal-guide-examples-and-prevention/
func verifyPath(path, trustedRoot string) (string, error) {
	// clean is already used upstream but leaving this
	// to keep the functionality of the function as close as possible to the blog.
	c := filepath.Clean(path)

	// get the canonical path
	r, err := filepath.EvalSymlinks(c)
	if err != nil {
		fmt.Println("Error " + err.Error())
		return c, errors.New("unsafe or invalid path specified")
	}

	if !inTrustedRoot(r, trustedRoot) {
		return r, errors.New("unsafe or invalid path specified")
	}

	return r, nil
}

func inTrustedRoot(path string, trustedRoot string) bool {
	return strings.HasPrefix(path, trustedRoot)
}
