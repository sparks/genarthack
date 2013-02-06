package main

import (
	"archive/zip"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	resourcePath = "templates/resources/"
	templatePath = "templates/"
	uploadPath   = "uploads/"
)

var templates = template.Must(template.ParseFiles(templatePath + "status.html"))
var usernameValidator = regexp.MustCompile("^[a-zA-Z0-9_. ]+$")

type StatusPage struct {
	Message string
}

type Submission struct {
	username  string
	timestamp time.Time
}

var usercount int = 0

func main() {
	err := os.Mkdir(uploadPath, os.ModeDir|0755)
	if err != nil && !os.IsExist(err) {
		panic(err)
	}

	http.HandleFunc("/", MainHandler)
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {})
	http.HandleFunc("/submit", SubmitHandler)
	http.Handle("/resources/", http.StripPrefix("/resources", http.FileServer(http.Dir(resourcePath))))
	http.Handle("/uploads/", http.StripPrefix("/uploads", http.FileServer(http.Dir(uploadPath))))

	http.ListenAndServe(":8080", nil)
}

func MainHandler(w http.ResponseWriter, r *http.Request) {
	listing, err := ioutil.ReadDir(uploadPath)

	if err != nil || len(listing) == 0 {
		ServeStatus(w, &StatusPage{"No content"})
		return
	}

	dirs := make([]os.FileInfo, 0, len(listing))

	for _, fileInfo := range listing {
		if fileInfo.IsDir() {
			dirs = dirs[0 : len(dirs)+1]
			dirs[len(dirs)-1] = fileInfo
		}
	}

	if len(dirs) == 0 {
		ServeStatus(w, &StatusPage{"No content"})
		return
	}

	http.Redirect(w, r, "/"+uploadPath+dirs[usercount].Name()+"/live/", http.StatusFound)

	usercount++
	if usercount >= len(dirs) {
		usercount = 0
	}
}

func SubmitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		http.ServeFile(w, r, templatePath+"submit.html")
	} else if r.Method == "POST" {
		t := time.Now()
		timestamp := t.Format("2006-01-02 15:04:05.000")

		username := r.FormValue("username")

		if !usernameValidator.MatchString(username) {
			ServeStatus(w, &StatusPage{"Invalid Username"})
			return
		}

		uploadFile, header, err := r.FormFile("file")
		if err != nil {
			ServeStatus(w, &StatusPage{"Invalid or Missing File"})
			return
		}

		if !strings.HasSuffix(header.Filename, ".zip") {
			ServeStatus(w, &StatusPage{"Non Zip File"})
			return
		}

		userDir := uploadPath + username + "/"

		userArchiveDir := userDir + "archive/"

		err = os.MkdirAll(userArchiveDir, os.ModeDir|0755)
		if err != nil && !os.IsExist(err) {
			ServeStatus(w, &StatusPage{"Error Making User Directories"})
			return
		}

		zipPath := userArchiveDir + timestamp + ".zip"

		osFile, err := os.Create(zipPath)
		if err != nil {
			ServeStatus(w, &StatusPage{"Error Creating File"})
			return
		}

		io.Copy(osFile, uploadFile)
		osFile.Close()

		// Open a zip archive for reading.
		zipReader, err := zip.OpenReader(zipPath)
		if err != nil {
			ServeStatus(w, &StatusPage{"Malformed Zip"})
			return
		}
		defer zipReader.Close()

		tmpUnzipDir := userDir + "tmp/"

		err = os.Mkdir(tmpUnzipDir, os.ModeDir|0755)
		if err != nil && !os.IsExist(err) {
			ServeStatus(w, &StatusPage{"Error Making Unzip Directory"})
			return
		}

		fileErr := false
		zipErr := false

		for _, zipFile := range zipReader.File {
			if strings.HasSuffix(zipFile.Name, "/") {
				os.MkdirAll(tmpUnzipDir+zipFile.Name, os.ModeDir|0755)
			} else {
				newFile, err := os.Create(tmpUnzipDir + zipFile.Name)
				if err != nil {
					panic(err)
					fileErr = true
				}

				zipFileReader, err := zipFile.Open()
				if err != nil {
					panic(err)
					zipErr = true
				}

				_, err = io.Copy(newFile, zipFileReader)
				if err != nil {
					panic(err)
					fileErr = true
				}

				zipFileReader.Close()
				newFile.Close()
			}
		}

		if zipErr || fileErr {
			p := &StatusPage{"Errors Unzipping:"}

			if zipErr {
				p.Message += " zip malformed"
			}

			if fileErr {
				p.Message += " couldn't create file"
			}

			os.RemoveAll(tmpUnzipDir)

			ServeStatus(w, p)
			return
		}

		userLiveDir := userDir + "live/"
		os.RemoveAll(userLiveDir)
		err = os.Rename(tmpUnzipDir, userLiveDir)
		if err != nil {
			ServeStatus(w, &StatusPage{"Problem Staging Project"})
			return
		}

		ServeStatus(w, &StatusPage{"Sucessful Upload"})
	}
}

func ServeStatus(w http.ResponseWriter, s *StatusPage) {
	err := templates.ExecuteTemplate(w, "status.html", s)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
