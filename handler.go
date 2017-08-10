package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	tmpl     *template.Template
	tmplName = "list.tmpl"

	tmplFuncs = template.FuncMap{
		"humansize": func(s int64) string {
			return humansize(s)
		},
		"mergepath": func(a ...string) string {
			return path.Join(a...)
		},
		"contenttype": func(path string, f os.FileInfo) string {
			// Try finding the content type based off the extension
			if s := detectByName(f.Name()); s != "" {
				return fmt.Sprintf("%s file", s)
			}

			// If not, open the file and read it, then get the contents
			return "File"
		},
		"prettytime": func(t time.Time) string {
			return t.Format(time.RFC1123)
		},
	}
)

func init() {
	// Find the folder to the current binary
	binaryPath := "./"
	if p, _ := os.Executable(); p != "" {
		binaryPath = path.Dir(p)
	}

	// Open the template
	tmpl = template.Must(template.New(tmplName).
		Funcs(tmplFuncs).
		ParseFiles(filepath.Join(binaryPath, tmplName)))
}

func handler(path string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// If the method is GET, then we continue, we fail with "Method Not Allowed"
		// otherwise, since all request are for files.
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		// Check if the URL ends on "/index.html", if so, redirect to the folder, because
		// we can handle it later down the road
		if strings.HasSuffix(r.URL.Path, "/index.html") {
			http.Redirect(w, r, strings.TrimSuffix(r.URL.Path, "index.html"), http.StatusMovedPermanently)
			return
		}

		// Get the full path to the file or directory, since
		// we don't need the current working directory, we can
		// omit the error
		fullpath, _ := filepath.Abs(filepath.Join(path, r.URL.Path))

		// Find if there's a file or folder here
		info, err := os.Stat(fullpath)
		if err != nil {
			// If when trying to stat a file, the error is "not exists"
			// then we throw a 404
			if os.IsNotExist(err) {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Check if it's a folder, if so, walk and present the contents on screen
		if info.IsDir() || strings.HasSuffix(fullpath, "/index.html") {
			if info.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
				http.Redirect(w, r, fmt.Sprintf("%s/", r.URL.Path), http.StatusFound)
				return
			}

			walk(fullpath, w, r)
			return
		}

		serve(fullpath, w, r)
	}
}

func serve(path string, w http.ResponseWriter, r *http.Request) {
	// If there's no info coming, we get it
	info, err := os.Stat(path)
	if err != nil {
		// If when trying to stat a file, the error is "not exists"
		// then we throw a 404
		if os.IsNotExist(err) {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Since http.ServeContent can only handle ReadSeekers then we
	// open the file for read.
	f, err := os.Open(path)

	// If we couldn't open, we just fail
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// We serve the content and close the file. ServeContent will handle
	// different headers like Range, Mime types and so on.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	f.Close()
}

func walk(fpath string, w http.ResponseWriter, r *http.Request) {
	// Check if there's an index file, and if so, present it on screen
	indexPath := filepath.Join(fpath, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		serve(indexPath, w, r)
		return
	}

	// If not, construct the UI we need with a list of files from this folder
	// by first opening the folder to get a Go object
	folder, err := os.Open(fpath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Then listing all the files in it (by passing -1 meaning all)
	list, err := folder.Readdir(-1)
	folder.Close()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Folders first, then alphabetically
	sort.Sort(foldersFirst(list))

	// Get the path to a parent folder
	parentFolder := ""
	if p := r.URL.Path; p != "/" {
		// If the path is not root, we're in a folder, but since folders
		// are enforced to use trailing slash then we need to remove it
		// so path.Dir() can work
		parentFolder = path.Dir(strings.TrimSuffix(p, "/"))
	}

	// If we reached this point, we're ready to print the template
	// so we create a bag, and we save the information there
	bag := map[string]interface{}{
		"Path":        r.URL.Path,
		"IncludeBack": parentFolder != "",
		"BackURL":     parentFolder,
		"Files":       list,
		"FilePath":    fpath,
	}
	if err := tmpl.ExecuteTemplate(w, tmplName, bag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
